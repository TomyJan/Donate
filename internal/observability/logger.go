package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

type guardedWriter struct {
	writer  io.Writer
	health  *fileHealth
	channel string
}

type fileHealth struct {
	mu       sync.Mutex
	failed   map[string]bool
	degraded *atomic.Bool
	app      string
}

func (h *fileHealth) report(channel string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	was := h.failed[channel]
	h.failed[channel] = err != nil
	anyFailed := false
	for _, failed := range h.failed {
		anyFailed = anyFailed || failed
	}
	h.degraded.Store(anyFailed)
	if err != nil && !was {
		fmt.Fprintf(os.Stderr, "ERROR 应用 %s 的 %s 日志写入失败，请检查磁盘与权限\n", h.app, channel)
	}
	if err == nil && was {
		fmt.Fprintf(os.Stderr, "INFO 应用 %s 的 %s 日志写入已恢复\n", h.app, channel)
	}
}

func (w guardedWriter) Write(p []byte) (int, error) {
	n, e := w.writer.Write(p)
	w.health.report(w.channel, e)
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

type Options struct {
	Outputs             []string
	Dir, AppName, Level string
	RetentionDays       int
}

func New(c Options, stdout io.Writer) (*slog.Logger, func() error, *atomic.Bool, error) {
	var l slog.Level
	if e := l.UnmarshalText([]byte(c.Level)); e != nil {
		return nil, nil, nil, e
	}
	if c.RetentionDays < 0 || len(c.AppName) > 63 || !regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`).MatchString(c.AppName) {
		return nil, nil, nil, errors.New("日志名称或保留天数无效")
	}
	if c.Outputs == nil {
		c.Outputs = []string{"console", "log", "jsonl"}
	}
	selected := map[string]bool{}
	for _, output := range c.Outputs {
		output = strings.TrimSpace(output)
		if output != "console" && output != "log" && output != "jsonl" {
			return nil, nil, nil, errors.New("日志输出仅支持 console、log、jsonl")
		}
		selected[output] = true
	}
	if len(selected) == 0 {
		return nil, nil, nil, errors.New("至少选择一个日志输出目的")
	}
	degraded := &atomic.Bool{}
	health := &fileHealth{failed: map[string]bool{}, degraded: degraded, app: c.AppName}
	opts := &slog.HandlerOptions{Level: l}
	var files []*dailyWriter
	close := func() error {
		var err error
		for _, file := range files {
			err = errors.Join(err, file.Close())
		}
		return err
	}
	var handlers []slog.Handler
	for _, output := range []string{"console", "log", "jsonl"} {
		if !selected[output] {
			continue
		}
		if output == "console" {
			handlers = append(handlers, slog.NewTextHandler(stdout, opts))
			continue
		}
		file := &dailyWriter{dir: c.Dir, name: c.AppName, extension: output, retention: c.RetentionDays, maxBytes: maxLogBytes}
		files = append(files, file)
		if _, err := file.Write(nil); err != nil {
			close()
			return nil, nil, nil, err
		}
		writer := guardedWriter{file, health, output}
		if output == "jsonl" {
			handlers = append(handlers, slog.NewJSONHandler(writer, opts))
		} else {
			handlers = append(handlers, slog.NewTextHandler(writer, opts))
		}
	}
	logger := slog.New(fanout{handlers}).With("app", c.AppName)
	return logger, close, degraded, nil
}
