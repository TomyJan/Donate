package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDailyLogsMatchConsoleAndJSON(t *testing.T) {
	var console bytes.Buffer
	dir := t.TempDir()
	l, close, _, err := New(Options{Dir: dir, AppName: "my-app", Level: "INFO", RetentionDays: 30}, &console)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("订单 order-one 的任务 job-two 已完成", "order_id", "order-one")
	if err := close(); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(dir, "my-app_"+time.Now().Format("2006-01-02"))
	text, err := os.ReadFile(prefix + ".log")
	if err != nil || string(text) != console.String() {
		t.Fatal("text file differs from console", string(text), err)
	}
	raw, err := os.ReadFile(prefix + ".jsonl")
	var entry map[string]any
	if err != nil || json.Unmarshal(bytes.TrimSpace(raw), &entry) != nil || entry["order_id"] != "order-one" || !strings.Contains(entry["msg"].(string), "job-two") {
		t.Fatal(string(raw), err)
	}
}

func TestDailyWriterRotatesResumesAndRetainsOnlyItsFiles(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"my-app_2026-10-01.log", "other_2026-10-01.log", "my-app_2026-10-01.log.notes", "my-app_2026-10-31.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := &dailyWriter{dir: dir, name: "my-app", extension: "log", retention: 2, maxBytes: 8}
	if _, err := w.writeAt([]byte("123456"), at); err != nil {
		t.Fatal(err)
	}
	if _, err := w.writeAt([]byte("abcd"), at); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = &dailyWriter{dir: dir, name: "my-app", extension: "log", retention: 2, maxBytes: 8}
	defer w.Close()
	if _, err := w.writeAt([]byte("ef"), at); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "my-app_2026-11-01.log.2"))
	if err != nil || string(got) != "abcdef" {
		t.Fatal("restart did not append last part", string(got), err)
	}
	if _, err := w.writeAt([]byte("next"), at.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "my-app_2026-10-01.log")); !os.IsNotExist(err) {
		t.Fatal("expired log retained", err)
	}
	for _, name := range []string{"other_2026-10-01.log", "my-app_2026-10-01.log.notes", "my-app_2026-11-01.log", "my-app_2026-11-02.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("unrelated or recent file removed", name, err)
		}
	}
}

func TestDailyWriterRejectsOversizedRecordWithoutSplitting(t *testing.T) {
	w := &dailyWriter{dir: t.TempDir(), name: "donate", extension: "jsonl", maxBytes: 8}
	defer w.Close()
	if _, err := w.Write([]byte("ninebytes")); err == nil {
		t.Fatal("oversized JSON record was split or exceeded limit")
	}
}

func TestHealthyChannelDoesNotClearAnotherLogFailure(t *testing.T) {
	flag := &atomic.Bool{}
	health := &fileHealth{failed: map[string]bool{}, degraded: flag, app: "donate"}
	bad := guardedWriter{writer: failingLogWriter{}, health: health, channel: "jsonl"}
	good := guardedWriter{writer: io.Discard, health: health, channel: "text"}
	_, _ = bad.Write([]byte("record"))
	_, _ = good.Write([]byte("record"))
	if !flag.Load() {
		t.Fatal("healthy text log hid JSONL failure")
	}
	bad.writer = io.Discard
	_, _ = bad.Write([]byte("record"))
	if flag.Load() {
		t.Fatal("recovered channels stayed degraded")
	}
}

type failingLogWriter struct{}

func (failingLogWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestLogFileLimitIsTwentyMiB(t *testing.T) {
	dir := t.TempDir()
	w := &dailyWriter{dir: dir, name: "donate", extension: "log", maxBytes: maxLogBytes}
	defer w.Close()
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := w.writeAt(make([]byte, 20*1024*1024), at); err != nil {
		t.Fatal(err)
	}
	if _, err := w.writeAt([]byte("next"), at); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int64{"donate_2026-11-01.log": 20971520, "donate_2026-11-01.log.2": 4} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() != size {
			t.Fatal(name, info, err)
		}
	}
}

func TestFanoutContinuesAfterOneHandlerFails(t *testing.T) {
	var console bytes.Buffer
	health := &fileHealth{failed: map[string]bool{}, degraded: &atomic.Bool{}, app: "donate"}
	h := fanout{handlers: []slog.Handler{slog.NewTextHandler(guardedWriter{failingLogWriter{}, health, "text"}, nil), slog.NewTextHandler(&console, nil)}}
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "订单 order-one 的日志仍然输出", 0)
	if err := h.Handle(context.Background(), record); err == nil || !strings.Contains(console.String(), "order-one") {
		t.Fatal("log failure suppressed console", console.String(), err)
	}
}
