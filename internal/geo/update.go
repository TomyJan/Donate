package geo

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/oschwald/maxminddb-golang/v2"
)

type databaseSource struct{ kind, path, url string }

type Updater struct {
	HTTP    *http.Client
	service *Service
	config  config.Config
	log     *slog.Logger
	mu      sync.Mutex
}

func NewUpdater(s *Service, c config.Config, l *slog.Logger) *Updater {
	if l == nil {
		l = slog.Default()
	}
	return &Updater{service: s, config: c, log: l, HTTP: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("下载重定向次数过多")
		}
		if req.URL.Scheme != "https" && !(c.Development && req.URL.Scheme == "http") {
			return errors.New("下载重定向必须使用 HTTPS")
		}
		// Go also trusts subdomains and other ports by default. Download
		// credentials belong only to the explicitly configured origin.
		if !sameDownloadOrigin(req.URL, via[0].URL) {
			req.Header.Del("Authorization")
			req.URL.User = nil
		}
		return nil
	}}}
}

func sameDownloadOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func databaseSources(c config.Config) []databaseSource {
	return []databaseSource{{"xdb4", c.GeoXDBv4, c.GeoXDBv4URL}, {"xdb6", c.GeoXDBv6, c.GeoXDBv6URL}, {"mmdb", c.GeoPath, c.GeoMMDBURL}}
}

func (u *Updater) enabled() bool { return u.config.GeoMode == "local" && u.config.GeoUpdateDays > 0 }

// Bootstrap downloads only missing explicitly enabled libraries. Existing data
// remains available immediately; its periodic refresh runs after startup.
func Bootstrap(ctx context.Context, c config.Config, l *slog.Logger) error {
	u := NewUpdater(nil, c, l)
	if !u.enabled() {
		return nil
	}
	for _, lib := range databaseSources(c) {
		if lib.path == "" || lib.url == "" {
			continue
		}
		if _, err := os.Stat(lib.path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查 %s 库文件失败", lib.kind)
		}
		if err := u.update(ctx, lib); err != nil {
			return fmt.Errorf("首次下载 %s 库失败: %w", lib.kind, err)
		}
	}
	return nil
}

func (u *Updater) Run(ctx context.Context) {
	if !u.enabled() {
		return
	}
	_ = u.Check(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = u.Check(ctx)
		}
	}
}

func (u *Updater) Check(ctx context.Context) error {
	if !u.enabled() {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	var failures []error
	now := time.Now()
	cutoff := now.AddDate(0, 0, -u.config.GeoUpdateDays)
	for _, lib := range databaseSources(u.config) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lib.path == "" || lib.url == "" {
			continue
		}
		info, err := os.Stat(lib.path)
		if err == nil && info.ModTime().After(cutoff) && !info.ModTime().After(now) {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Errorf("检查 %s 库文件失败", lib.kind))
			continue
		}
		if err := u.update(ctx, lib); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			u.log.Warn("属地库 "+lib.kind+" 更新失败，继续使用旧库；目标 "+lib.path, "source", lib.kind, "path", lib.path, "error", err)
			failures = append(failures, fmt.Errorf("%s 更新失败: %w", lib.kind, err))
		}
	}
	return errors.Join(failures...)
}

func (u *Updater) update(parent context.Context, lib databaseSource) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	stage, err := u.download(ctx, lib)
	if err != nil {
		return err
	}
	defer os.Remove(stage)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if u.service == nil {
		if err = os.Rename(stage, lib.path); err != nil {
			return errors.New("无法安装首次下载的库文件")
		}
	} else if err = u.service.installDatabase(ctx, lib, stage); err != nil {
		return err
	}
	u.log.Info("属地库 "+lib.kind+" 已校验并更新，文件 "+lib.path, "source", lib.kind, "path", lib.path)
	return nil
}

func validateDatabase(path, kind string) error {
	return validateDatabaseContext(context.Background(), path, kind)
}

func validateDatabaseContext(ctx context.Context, path, kind string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind == "mmdb" {
		r, err := maxminddb.Open(path)
		if err != nil {
			return errors.New("MMDB 文件无法读取")
		}
		defer r.Close()
		if err := r.Verify(); err != nil {
			return errors.New("MMDB 完整性校验失败")
		}
		// Structural integrity alone does not imply a City-compatible schema.
		// ISP and anonymous-IP products are valid MMDBs but cannot locate users.
		switch strings.ToLower(r.Metadata.DatabaseType) {
		case "geoip2-city", "geolite2-city", "geoip2-enterprise",
			"dbip-city-lite", "dbip-city", "dbip-location", "dbip-location-isp", "dbip-full":
			return nil
		default:
			return errors.New("需要兼容的 City MMDB，不能使用 Country / ASN / 独立 ISP 等库")
		}
	}
	version := xdb.IPv4
	if kind == "xdb6" {
		version = xdb.IPv6
	} else if kind != "xdb4" {
		return errors.New("库类型无效")
	}
	r, err := openXDB(path, version)
	if err != nil {
		return err
	}
	r.close()
	return verifyXDBRecords(ctx, path, version)
}

func verifyXDBRecords(ctx context.Context, path string, version *xdb.Version) error {
	header, err := xdb.LoadHeaderFromFile(path)
	if err != nil {
		return errors.New("XDB 头读取失败")
	}
	f, err := os.Open(path)
	if err != nil {
		return errors.New("XDB 记录无法读取")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return errors.New("XDB 大小无法读取")
	}
	size := int64(version.SegmentIndexSize)
	length := int64(header.EndIndexPtr) - int64(header.StartIndexPtr) + size
	vectors := make([]byte, 256*256*8)
	if _, err := f.ReadAt(vectors, xdb.HeaderInfoLength); err != nil {
		return errors.New("XDB 向量索引读取失败")
	}
	r := bufio.NewReaderSize(contextReader{ctx, io.NewSectionReader(f, int64(header.StartIndexPtr), length)}, 64*1024)
	segment := make([]byte, size)
	previousEnd := make([]byte, version.Bytes)
	currentStart := make([]byte, version.Bytes)
	copyAddress := func(dst, src []byte) {
		copy(dst, src)
		// The SDK compares network-order input with little-endian IPv4 indexes.
		if version.Id == xdb.IPv4VersionNo {
			for i, j := 0, len(dst)-1; i < j; i, j = i+1, j-1 {
				dst[i], dst[j] = dst[j], dst[i]
			}
		}
	}
	for offset := int64(0); offset < length; offset += size {
		if _, err := io.ReadFull(r, segment); err != nil {
			return errors.New("XDB 分段索引截断或读取失败")
		}
		start, end := segment[:version.Bytes], segment[version.Bytes:version.Bytes*2]
		copyAddress(currentStart, start)
		if version.IPCompare(currentStart, end) > 0 {
			return errors.New("XDB 地址区间起止倒置")
		}
		if offset > 0 && version.IPCompare(previousEnd, start) >= 0 {
			return errors.New("XDB 地址区间乱序或重叠")
		}
		copyAddress(previousEnd, end)
		// Sparse prefixes may be empty, but every stored address interval must
		// be reachable through each prefix it intersects. Pointer bounds alone
		// cannot detect a zeroed or incorrectly narrowed vector table.
		first := int(currentStart[0])*256 + int(currentStart[1])
		last := int(previousEnd[0])*256 + int(previousEnd[1])
		pointer := uint64(header.StartIndexPtr) + uint64(offset)
		for prefix := first; prefix <= last; prefix++ {
			vector := vectors[prefix*8:]
			a := uint64(binary.LittleEndian.Uint32(vector))
			b := uint64(binary.LittleEndian.Uint32(vector[4:]))
			if a > pointer || b < pointer {
				return errors.New("XDB 向量索引未覆盖地址区间")
			}
		}
		at := version.Bytes * 2
		dataLength := int64(binary.LittleEndian.Uint16(segment[at:]))
		dataOffset := int64(binary.LittleEndian.Uint32(segment[at+2:]))
		if dataLength > 0 && (dataOffset < xdb.HeaderInfoLength+256*256*8 || dataOffset+dataLength > info.Size()) {
			return errors.New("XDB 记录数据越界或截断")
		}
	}
	return ctx.Err()
}

func (s *Service) installDatabase(ctx context.Context, lib databaseSource, stage string) error {
	if err := validateDatabaseContext(ctx, stage, lib.kind); err != nil {
		return err
	}
	s.localMu.Lock()
	defer s.localMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.closed {
		return ErrUnavailable
	}
	expected := s.config.GeoPath
	if lib.kind == "xdb4" {
		expected = s.config.GeoXDBv4
	} else if lib.kind == "xdb6" {
		expected = s.config.GeoXDBv6
	}
	actual, _ := filepath.Abs(lib.path)
	want, _ := filepath.Abs(expected)
	if expected == "" || actual != want {
		return errors.New("更新目标与当前本地库不一致")
	}
	backup := ""
	keepBackup := false
	if info, err := os.Lstat(lib.path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("库更新目标不是普通文件")
		}
		f, err := os.CreateTemp(filepath.Dir(lib.path), ".ipdb-rollback-*")
		if err != nil {
			return errors.New("无法创建库回滚副本")
		}
		backup = f.Name()
		defer func() {
			if !keepBackup {
				os.Remove(backup)
			}
		}()
		old, err := os.Open(lib.path)
		if err != nil {
			f.Close()
			return errors.New("无法读取原库文件")
		}
		_, copyErr := io.Copy(f, old)
		readClose := old.Close()
		syncErr := f.Sync()
		writeClose := f.Close()
		if errors.Join(copyErr, readClose, syncErr, writeClose) != nil {
			return errors.New("无法保存库回滚副本")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("无法检查原库文件")
	}
	closeReader := func() {
		switch lib.kind {
		case "xdb4":
			if s.xdb4 != nil {
				s.xdb4.close()
				s.xdb4 = nil
			}
		case "xdb6":
			if s.xdb6 != nil {
				s.xdb6.close()
				s.xdb6 = nil
			}
		case "mmdb":
			if s.local != nil {
				s.local.Close()
				s.local = nil
			}
		}
	}
	openReader := func() error {
		var err error
		switch lib.kind {
		case "xdb4":
			s.xdb4, err = openXDB(lib.path, xdb.IPv4)
		case "xdb6":
			s.xdb6, err = openXDB(lib.path, xdb.IPv6)
		case "mmdb":
			s.local, err = maxminddb.Open(lib.path)
		default:
			err = errors.New("库类型无效")
		}
		return err
	}
	closeReader()
	if err := os.Rename(stage, lib.path); err != nil {
		if reopen := openReader(); reopen != nil {
			return errors.Join(errors.New("库替换失败且旧库无法重新打开"), reopen)
		}
		return errors.New("库替换失败，已继续使用旧库")
	}
	if err := openReader(); err != nil {
		if backup != "" {
			if rollback := os.Rename(backup, lib.path); rollback != nil {
				keepBackup = true
				return fmt.Errorf("库重载和回滚失败，旧库副本保留在 %s", backup)
			}
			if reopen := openReader(); reopen != nil {
				return errors.Join(errors.New("库回滚后无法重新打开"), reopen)
			}
		}
		return errors.New("新库无法重载，已回滚")
	}
	s.mu.Lock()
	s.revision++
	s.generation++
	s.cache = map[string]cached{}
	s.mu.Unlock()
	return nil
}
