package observability

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxLogBytes int64 = 20 * 1024 * 1024

type dailyWriter struct {
	mu                   sync.Mutex
	dir, name, extension string
	retention            int
	maxBytes             int64
	date                 string
	part                 int
	file                 *os.File
	size                 int64
	closed               bool
}

func (w *dailyWriter) Write(p []byte) (int, error) { return w.writeAt(p, time.Now()) }

func (w *dailyWriter) writeAt(p []byte, now time.Time) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	if int64(len(p)) > w.maxBytes {
		return 0, errors.New("单条日志超过单文件容量，保留控制台输出")
	}
	date := now.Format("2006-01-02")
	if w.date != date {
		if w.file != nil {
			if err := w.file.Close(); err != nil {
				return 0, err
			}
			w.file = nil
		}
		if err := os.MkdirAll(w.dir, 0700); err != nil {
			return 0, err
		}
		entries, err := os.ReadDir(w.dir)
		if err != nil {
			return 0, err
		}
		part := 1
		cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-w.retention)
		for _, entry := range entries {
			day, extension, n, ok := logFilename(entry.Name(), w.name)
			if !ok || extension != w.extension || !entry.Type().IsRegular() {
				continue
			}
			if day == date && n > part {
				part = n
			}
			at, err := time.ParseInLocation("2006-01-02", day, now.Location())
			if err == nil && w.retention > 0 && at.Before(cutoff) {
				if err := os.Remove(filepath.Join(w.dir, entry.Name())); err != nil {
					fmt.Fprintf(os.Stderr, "WARN 应用 %s 的过期日志 %s 清理失败，请检查权限\n", w.name, entry.Name())
				}
			}
		}
		w.date, w.part = date, part
	}
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > w.maxBytes {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		w.file = nil
		w.part++
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *dailyWriter) open() error {
	path := filepath.Join(w.dir, w.name+"_"+w.date+"."+w.extension)
	if w.part > 1 {
		path += "." + strconv.Itoa(w.part)
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("日志路径不是普通文件")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

func logFilename(filename, app string) (day, extension string, part int, ok bool) {
	rest, found := strings.CutPrefix(filename, app+"_")
	if !found || len(rest) < 14 {
		return
	}
	day = rest[:10]
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return
	}
	rest = rest[10:]
	for _, ext := range []string{".log", ".jsonl"} {
		if tail, found := strings.CutPrefix(rest, ext); found {
			extension = ext[1:]
			if tail == "" {
				return day, extension, 1, true
			}
			if strings.HasPrefix(tail, ".") {
				if n, err := strconv.Atoi(tail[1:]); err == nil && n >= 2 && strconv.Itoa(n) == tail[1:] {
					return day, extension, n, true
				}
			}
		}
	}
	return "", "", 0, false
}
