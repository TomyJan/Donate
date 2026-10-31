package observability

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnlySelectedLogOutputsReceiveRecords(t *testing.T) {
	names := []string{"console", "log", "jsonl"}
	for mask := 1; mask < 8; mask++ {
		var outputs []string
		for i, name := range names {
			if mask&(1<<i) != 0 {
				outputs = append(outputs, name)
			}
		}
		t.Run(strings.Join(outputs, ","), func(t *testing.T) {
			var console bytes.Buffer
			dir := filepath.Join(t.TempDir(), "logs")
			l, close, health, err := New(Options{Dir: dir, AppName: "donate", Level: "INFO", Outputs: outputs, RetentionDays: 30}, &console)
			if err != nil {
				t.Fatal(err)
			}
			l.Info("订单 order-123 已支付", "order_id", "order-123")
			if err := close(); err != nil {
				t.Fatal(err)
			}
			if health.Load() {
				t.Fatal("selected outputs incorrectly degraded")
			}
			if got := strings.Count(console.String(), "订单 order-123 已支付"); got != mask&1 {
				t.Fatal("console output selection ignored", got)
			}
			for i, ext := range []string{"log", "jsonl"} {
				data, err := os.ReadFile(filepath.Join(dir, "donate_"+time.Now().Format("2006-01-02")+"."+ext))
				if mask&(1<<(i+1)) != 0 {
					if err != nil || !bytes.Contains(data, []byte("order-123")) {
						t.Fatal(ext, string(data), err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatal("unselected file created", ext, err)
				}
			}
			if mask == 1 {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatal("console-only logging touched the filesystem", err)
				}
			}
		})
	}
}

func TestConsoleLoggingDoesNotRequireLogDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	l, close, _, err := New(Options{Dir: path, AppName: "donate", Level: "INFO", Outputs: []string{"console"}}, &out)
	if err != nil {
		t.Fatal("console-only logging required writable log directory", err)
	}
	l.Info("任务 job-123 执行完成")
	if err := close(); err != nil || !strings.Contains(out.String(), "job-123") {
		t.Fatal(err, out.String())
	}
}

func TestInvalidOutputsAreRejectedBeforeCreatingFiles(t *testing.T) {
	for _, outputs := range [][]string{{}, {"file"}, {"console", ""}} {
		dir := filepath.Join(t.TempDir(), "logs")
		_, close, _, err := New(Options{Dir: dir, AppName: "donate", Level: "INFO", Outputs: outputs}, &bytes.Buffer{})
		if err == nil {
			close()
			t.Fatal("invalid outputs accepted", outputs)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("invalid outputs created files", err)
		}
	}
}

func TestDuplicateOutputDoesNotDuplicateRecords(t *testing.T) {
	var console bytes.Buffer
	l, close, _, err := New(Options{Dir: t.TempDir(), AppName: "donate", Level: "INFO", Outputs: []string{"console", "console"}}, &console)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("订单 order-once 已确认")
	if err := close(); err != nil || strings.Count(console.String(), "order-once") != 1 {
		t.Fatal(err, console.String())
	}
}
