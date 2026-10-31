package observability

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsoleAndFileKeepIdentifyingMessage(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	l, close, _, err := New(Options{Dir: dir, AppName: "donate", Level: "INFO", RetentionDays: 30}, &out)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("订单 order-123 的任务 job-456 已完成", "order_id", "order-123")
	if err = close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "donate_"+time.Now().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{out.String(), string(b)} {
		if !strings.Contains(v, "order-123") || !strings.Contains(v, "job-456") {
			t.Fatal("missing prose identity", v)
		}
	}
}
