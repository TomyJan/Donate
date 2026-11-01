package geo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
)

func TestCancelledUpdateKeepsOldFileAndCleansDownloads(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|旧数据|CN")
	before, _ := os.ReadFile(path)
	c := config.Config{Development: true, GeoMode: "local", GeoXDBv4: path, GeoXDBv4URL: "http://example.com/db", GeoUpdateDays: 1}
	s, err := New(c, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u := NewUpdater(s, c, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	u.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &cancelledBody{cancel: cancel}, Header: make(http.Header)}, nil
	})
	if err := u.update(ctx, databaseSources(c)[0]); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("cancelled download changed original file")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".ipdb-*"))
	if len(files) != 0 {
		t.Fatal("download left temporary files", files)
	}
	if p, err := s.Refresh(context.Background(), "1.1.1.1"); err != nil || p.Province != "广东" {
		t.Fatal(p, err)
	}
}

type cancelledBody struct{ cancel context.CancelFunc }

func (r *cancelledBody) Read(p []byte) (int, error) { r.cancel(); return 0, context.Canceled }
func (r *cancelledBody) Close() error               { return nil }

func TestDownloadRejectsOversizeResponseAndWrongProtocol(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int64
		body   []byte
		kind   string
	}{
		{"oversize", maxDatabaseBytes + 1, nil, "xdb4"},
		{"wrong protocol", -1, mustReadDatabase(t, syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN")), "xdb6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			u := NewUpdater(nil, config.Config{}, slog.Default())
			u.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: tc.length, Body: io.NopCloser(bytes.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			if _, err := u.download(context.Background(), databaseSource{tc.kind, filepath.Join(dir, "db"), "https://example.com/db?secret=hidden"}); err == nil || strings.Contains(err.Error(), "hidden") {
				t.Fatal(err)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("invalid download left files", files)
			}
		})
	}
}

func mustReadDatabase(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUpdaterReloadsMMDBAndStopsOnCancellation(t *testing.T) {
	data := mustReadDatabase(t, "testdata/synthetic-city.mmdb")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "City.mmdb")
	c := config.Config{Development: true, GeoMode: "local", GeoPath: path, GeoMMDBURL: server.URL, GeoUpdateDays: 7}
	if err := downloadInitialForTest(context.Background(), c, slog.Default()); err != nil {
		t.Fatal(err)
	}
	s, err := New(c, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u := NewUpdater(s, c, slog.Default())
	if err := u.update(context.Background(), databaseSources(c)[2]); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Lookup(context.Background(), "223.5.5.5"); err != nil || p.Source != "mmdb" {
		t.Fatal(p, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { defer close(finished); _ = u.Update(ctx, "mmdb") }()
	cancel()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("updater ignored cancellation")
	}
}

func TestDownloadedMMDBMustPassFullIntegrityCheck(t *testing.T) {
	data := mustReadDatabase(t, "testdata/synthetic-city.mmdb")
	for i := range 6 {
		data[i] = 0xff
	}
	path := filepath.Join(t.TempDir(), "corrupt.mmdb")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateDatabase(path, "mmdb"); err == nil {
		t.Fatal("MMDB with corrupt search tree accepted")
	}
}

func TestTruncatedXDBDownloadPreservesOriginalLibrary(t *testing.T) {
	for _, tc := range []struct{ ip, kind string }{{"1.1.1.1", "xdb4"}, {"240e::1", "xdb6"}} {
		t.Run(tc.kind, func(t *testing.T) {
			path := syntheticXDB(t, tc.ip, "中国|广东|深圳|网络|CN")
			data := mustReadDatabase(t, path)
			c := config.Config{GeoMode: "local"}
			if tc.kind == "xdb4" {
				c.GeoXDBv4 = path
			} else {
				c.GeoXDBv6 = path
			}
			s, err := New(c, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			u := NewUpdater(s, c, slog.Default())
			u.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data[:len(data)-1])), Header: make(http.Header)}, nil
			})
			if err := u.update(context.Background(), databaseSource{tc.kind, path, "https://example.com/db"}); err == nil {
				t.Fatal("truncated XDB installed")
			}
			if current := mustReadDatabase(t, path); !bytes.Equal(current, data) {
				t.Fatal("broken download replaced previous file")
			}
			if p, err := s.Refresh(context.Background(), tc.ip); err != nil || p.Province != "广东" {
				t.Fatal(p, err)
			}
		})
	}
}

func TestConcurrentQueriesHotReloadAndClose(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN")
	c := config.Config{GeoMode: "local", GeoXDBv4: path}
	s, err := New(c, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 30 {
				p, err := s.Refresh(context.Background(), "1.1.1.1")
				if err != nil && !errors.Is(err, ErrUnavailable) {
					t.Error(err)
					return
				}
				if err == nil && p.Province != "广东" && p.Province != "浙江" {
					t.Error(p)
					return
				}
			}
		})
	}
	for range 3 {
		stage := syntheticXDB(t, "1.1.1.1", "中国|浙江|杭州|网络|CN")
		if err := s.installDatabase(context.Background(), databaseSources(c)[0], stage); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
	if p, err := s.Lookup(context.Background(), "1.1.1.1"); err != nil || p.Province != "浙江" {
		t.Fatal("stale cache after hot reload", p, err)
	}
	s.Close()
	stage := syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN")
	if err := s.installDatabase(context.Background(), databaseSources(c)[0], stage); !errors.Is(err, ErrUnavailable) {
		t.Fatal("update reopened a closed service", err)
	}
}

func TestCancelledInstallAndWrongTargetPreserveOriginalReader(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN")
	c := config.Config{GeoMode: "local", GeoXDBv4: path}
	s, err := New(c, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Cancellation and target mismatch must leave the live reader intact.
	lib := databaseSources(c)[0]
	stage := syntheticXDB(t, "1.1.1.1", "中国|浙江|杭州|网络|CN")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.installDatabase(ctx, lib, stage); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	lib.path = t.TempDir()
	if err := s.installDatabase(context.Background(), lib, stage); err == nil {
		t.Fatal("wrong target accepted")
	}
	if p, err := s.Refresh(context.Background(), "1.1.1.1"); err != nil || p.Province != "广东" {
		t.Fatal(p, err)
	}
}

func TestUpdaterBootstrapsAndHotReloadsWithoutOldCache(t *testing.T) {
	old, _ := os.ReadFile(syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|旧数据|CN"))
	newer, _ := os.ReadFile(syntheticXDB(t, "1.1.1.1", "中国|浙江|杭州|新数据|CN"))
	var data atomic.Value
	data.Store(old)
	var unavailable atomic.Bool
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if unavailable.Load() {
			http.Error(w, "upstream unavailable", 503)
			return
		}
		_, _ = w.Write(data.Load().([]byte))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ipdb", "v4.xdb")
	c := config.Config{Development: true, GeoMode: "local", GeoXDBv4: path, GeoXDBv4URL: server.URL + "?key=must-not-leak", GeoUpdateDays: 7}
	var logs bytes.Buffer
	l := slog.New(slog.NewTextHandler(&logs, nil))
	if err := downloadInitialForTest(context.Background(), c, l); err != nil {
		t.Fatal(err)
	}
	s, err := New(c, l)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Province != "广东" {
		t.Fatal(p, err)
	}
	u := NewUpdater(s, c, l)
	if err := checkDueUpdates(u, context.Background()); err != nil || calls.Load() != 1 {
		t.Fatal("fresh file downloaded again", calls.Load(), err)
	}
	data.Store(newer)
	at := time.Now().AddDate(0, 0, -8)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	if err := checkDueUpdates(u, context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err = s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Province != "浙江" {
		t.Fatal("hot reload retained old cached province", p, err)
	}
	unavailable.Store(true)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	if err := checkDueUpdates(u, context.Background()); err == nil {
		t.Fatal("failed download reported success")
	}
	p, err = s.Refresh(context.Background(), "1.1.1.1")
	if err != nil || p.Province != "浙江" {
		t.Fatal("failed update discarded previous file", p, err)
	}
	if strings.Contains(logs.String(), "must-not-leak") || strings.Contains(logs.String(), server.URL) {
		t.Fatal("download credentials or URL were logged", logs.String())
	}
}

func TestDisabledUpdatesNeverDownload(t *testing.T) {
	for _, c := range []config.Config{
		{GeoMode: "online", GeoXDBv4URL: "https://invalid.example/key", GeoUpdateDays: 7},
		{GeoMode: "disabled", GeoXDBv4URL: "https://invalid.example/key", GeoUpdateDays: 7},
		{GeoMode: "local", GeoXDBv4URL: "https://invalid.example/key", GeoUpdateDays: 0},
	} {
		if err := downloadInitialForTest(context.Background(), c, slog.Default()); err != nil {
			t.Fatal(err)
		}
		u := NewUpdater(nil, c, slog.Default())
		u.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
			t.Error("disabled update used network")
			return nil, ErrUnavailable
		})
		if err := checkDueUpdates(u, context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidUpdateKeepsUsableReaderAndOriginalFile(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|旧数据|CN")
	before, _ := os.ReadFile(path)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>not a database</html>")) }))
	defer server.Close()
	c := config.Config{Development: true, GeoMode: "local", GeoXDBv4: path, GeoXDBv4URL: server.URL, GeoUpdateDays: 1}
	s, err := New(c, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().AddDate(0, 0, -2)
	_ = os.Chtimes(path, at, at)
	if err := checkDueUpdates(NewUpdater(s, c, slog.Default()), context.Background()); err == nil {
		t.Fatal("HTML accepted as XDB")
	}
	after, _ := os.ReadFile(path)
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if !bytes.Equal(before, after) || err != nil || p.Province != "广东" {
		t.Fatal("invalid download changed live file", p, err)
	}
}

// Test orchestration drives the same due/update interface used by queue workers.
func checkDueUpdates(u *Updater, ctx context.Context) error {
	var failures []error
	for _, kind := range u.Due(time.Now()) {
		if err := u.Update(ctx, kind); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func downloadInitialForTest(ctx context.Context, c config.Config, l *slog.Logger) error {
	return checkDueUpdates(NewUpdater(nil, c, l), ctx)
}
