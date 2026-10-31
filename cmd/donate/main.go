package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/httpapi"
	"github.com/TomyJan/Donate/internal/jobs"
	"github.com/TomyJan/Donate/internal/observability"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/payment"
	"github.com/TomyJan/Donate/internal/storage"
	"github.com/TomyJan/Donate/internal/webassets"
	"github.com/gofrs/flock"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var commit = "unknown"
var tag = ""
var builtAt = ""

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "ERROR Donate 启动或运行失败:", e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("Donate V%s commit=%s tag=%s built_at=%s\n", version, commit, tag, builtAt)
		return nil
	}
	c, e := config.Load()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(c.DataDir, 0700); e != nil {
		return e
	}
	lock := flock.New(c.LockPath())
	ok, e := lock.TryLock()
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("数据目录已有 Donate 实例运行")
	}
	defer lock.Unlock()
	log, closeLog, degraded, e := observability.New(observability.Options{Dir: c.LogDir, AppName: c.AppName, Level: c.LogLevel, RetentionDays: c.LogRetentionDays}, os.Stdout)
	if e != nil {
		return e
	}
	defer closeLog()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	storageOptions := storage.Options{AppName: c.AppName, BackupDir: c.BackupDir, BackupRetentionDays: c.BackupRetentionDays}
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		if len(os.Args) != 3 {
			return errors.New("用法: donate restore <备份路径>；须先停止服务")
		}
		if e = storage.RestoreWithOptions(ctx, os.Args[2], c.DatabasePath(), storageOptions); e != nil {
			return e
		}
		log.Warn("数据库离线恢复完成，旧会话已撤销；请核实恢复点后的删除、封禁和支付变化")
		return nil
	}
	db, e := storage.OpenWithOptions(ctx, c.DatabasePath(), storageOptions)
	if e != nil {
		return e
	}
	defer db.Close()
	if len(os.Args) > 1 {
		if os.Args[1] == "backup" {
			p, e := db.Backup(ctx, c.BackupDir)
			if e == nil {
				fmt.Println(p)
			}
			return e
		}
		return errors.New("仅支持 --version / backup / restore <备份路径>")
	}
	locator, e := geo.New(c, log)
	if e != nil {
		return e
	}
	defer locator.Close()
	gateway, e := payment.NewGateway(c)
	if e != nil {
		return e
	}
	orders := order.New(db, log, c.EpayURL, c.EpayPID, c.EpayProtocol)
	authentication := auth.New(db, c, log)
	queue := jobs.NewQueue(db, log)
	assets, e := webassets.WithUmami(webassets.FS(), c.UmamiScriptURL, c.UmamiWebsiteID)
	if e != nil {
		return e
	}
	if c.UmamiScriptURL != "" {
		log.Info(fmt.Sprintf("已为站点 %s 启用 Umami 访问统计，统计服务 %s，网站 ID %s", c.PublicURL, c.UmamiOrigin, c.UmamiWebsiteID), "site", c.PublicURL, "analytics_origin", c.UmamiOrigin, "website_id", c.UmamiWebsiteID)
	}
	api := &httpapi.Server{Config: c, Auth: authentication, Orders: orders, Payments: &payment.Service{Orders: orders, Gateway: gateway}, Admin: admin.New(db, orders, c, log), Queue: queue, Log: log, Assets: assets, Version: httpapi.Version{Version: version, Commit: commit, Tag: tag, BuiltAt: builtAt}, Degraded: degraded}
	server := &http.Server{Addr: c.Listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 * 1024}
	runner := &jobs.Runner{Queue: queue, Orders: orders, Gateway: gateway, Geo: locator}
	done := make(chan struct{})
	go func() { defer close(done); runner.Run(ctx) }()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if p, e := db.Backup(ctx, c.BackupDir); e != nil {
					log.Error("每日数据库备份失败", "error", e)
				} else {
					log.Info(fmt.Sprintf("每日数据库备份已保存至 %s", filepath.Base(p)), "backup", filepath.Base(p))
				}
				_, _ = db.DB.ExecContext(ctx, "DELETE FROM auth_flows WHERE expires_at<?", time.Now().Unix())
				_, _ = db.DB.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<?", time.Now().Add(-24*time.Hour).Unix())
			}
		}
	}()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	log.Info(fmt.Sprintf("Donate V%s 提交 %s 已启动，监听 %s", version, commit, c.Listen), "version", version, "commit", commit, "listen", c.Listen, "payment_enabled", gateway.Ready())
	select {
	case <-ctx.Done():
	case e = <-errCh:
		if !errors.Is(e, http.ErrServerClosed) {
			stop()
			return e
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	e = server.Shutdown(shutdown)
	if e != nil {
		_ = server.Close()
	}
	select {
	case <-done:
	case <-shutdown.Done():
	}
	log.Info(fmt.Sprintf("Donate 提交 %s 已停止接收请求并结束任务", commit), "commit", commit)
	return e
}
