package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	"gopkg.in/natefinch/lumberjack.v2"
)

type guardedWriter struct {
	writer   io.Writer
	degraded *atomic.Bool
}

func (w guardedWriter) Write(p []byte) (int, error) {
	n, e := w.writer.Write(p)
	if e != nil {
		if !w.degraded.Swap(true) {
			fmt.Fprintln(os.Stderr, "ERROR 日志文件写入失败，运行日志降级到控制台，请检查磁盘与权限")
		}
	} else if w.degraded.Swap(false) {
		fmt.Fprintln(os.Stderr, "INFO 日志文件写入已恢复")
	}
	return n, e
}

type fanout struct{ handlers []slog.Handler }

func (h fanout) Enabled(c context.Context, l slog.Level) bool {
	for _, v := range h.handlers {
		if v.Enabled(c, l) {
			return true
		}
	}
	return false
}
func (h fanout) Handle(c context.Context, r slog.Record) error {
	var first error
	for _, v := range h.handlers {
		if v.Enabled(c, r.Level) {
			if e := v.Handle(c, r.Clone()); e != nil && first == nil {
				first = e
			}
		}
	}
	return first
}
func (h fanout) WithAttrs(a []slog.Attr) slog.Handler {
	v := fanout{}
	for _, x := range h.handlers {
		v.handlers = append(v.handlers, x.WithAttrs(a))
	}
	return v
}
func (h fanout) WithGroup(g string) slog.Handler {
	v := fanout{}
	for _, x := range h.handlers {
		v.handlers = append(v.handlers, x.WithGroup(g))
	}
	return v
}

func New(dir, level string, stdout io.Writer) (*slog.Logger, func() error, *atomic.Bool, error) {
	var l slog.Level
	if e := l.UnmarshalText([]byte(level)); e != nil {
		return nil, nil, nil, e
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, nil, nil, e
	}
	p := filepath.Join(dir, "donate.log")
	f, e := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return nil, nil, nil, e
	}
	if e = f.Close(); e != nil {
		return nil, nil, nil, e
	}
	rotation := &lumberjack.Logger{Filename: p, MaxSize: 50, MaxBackups: 10, MaxAge: 14, Compress: true}
	degraded := &atomic.Bool{}
	opts := &slog.HandlerOptions{Level: l}
	logger := slog.New(fanout{[]slog.Handler{slog.NewTextHandler(stdout, opts), slog.NewJSONHandler(guardedWriter{rotation, degraded}, opts)}})
	return logger, rotation.Close, degraded, nil
}
