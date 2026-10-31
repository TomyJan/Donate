package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
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
		fmt.Fprintf(os.Stderr, "ERROR 应用 %s 的 %s 日志写入失败，保留控制台输出，请检查磁盘与权限\n", h.app, channel)
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
	text := &dailyWriter{dir: c.Dir, name: c.AppName, extension: "log", retention: c.RetentionDays, maxBytes: maxLogBytes}
	structured := &dailyWriter{dir: c.Dir, name: c.AppName, extension: "jsonl", retention: c.RetentionDays, maxBytes: maxLogBytes}
	close := func() error { return errors.Join(text.Close(), structured.Close()) }
	for _, w := range []*dailyWriter{text, structured} {
		if _, err := w.Write(nil); err != nil {
			close()
			return nil, nil, nil, err
		}
	}
	degraded := &atomic.Bool{}
	health := &fileHealth{failed: map[string]bool{}, degraded: degraded, app: c.AppName}
	opts := &slog.HandlerOptions{Level: l}
	logger := slog.New(fanout{[]slog.Handler{slog.NewTextHandler(stdout, opts), slog.NewTextHandler(guardedWriter{text, health, "text"}, opts), slog.NewJSONHandler(guardedWriter{structured, health, "jsonl"}, opts)}}).With("app", c.AppName)
	return logger, close, degraded, nil
}
