package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestTaskRetryRollbackLogsSystemTaskAndRequest(t *testing.T) {
	s, _, actor, _ := taskFixture(t)
	configureTaskGeoIP(s)
	id := taskGeoIP(t, s, "xdb4", "failed")
	if _, err := s.Store.DB.Exec("CREATE TRIGGER reject_retry_audit BEFORE INSERT ON audit_events WHEN NEW.action='task.retry' BEGIN SELECT RAISE(ABORT,'private-audit-detail'); END"); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	if _, err := s.RetryTasks(context.Background(), actor, []string{id}, "private-idempotency-key", "retry-request"); err == nil {
		t.Fatal("failure ignored")
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("missing failure diagnostics: %s %v", logs.String(), err)
	}
	if entry["job_id"] != id || entry["resource"] != "xdb4" || entry["request_id"] != "retry-request" || entry["stage"] != "audit" || entry["reason"] != "sqlite_1811" {
		t.Fatal(entry)
	}
	if strings.Contains(logs.String(), "private-audit-detail") || strings.Contains(logs.String(), "private-idempotency-key") {
		t.Fatal("admin failure leaked secrets")
	}
}

func TestBanRejectionLogsTargetAndControlReason(t *testing.T) {
	s, _, actor, _ := taskFixture(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	if err := s.Ban(context.Background(), actor, actor, true); !errors.Is(err, ErrSelfBan) {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("missing denial diagnostics: %s %v", logs.String(), err)
	}
	if entry["user_id"] != actor || entry["actor_id"] != actor || entry["stage"] != "self_ban_guard" || entry["reason"] != "self_ban" || entry["level"] != "WARN" {
		t.Fatal(entry)
	}
}

func TestIdentityUnbindRollbackLogsIdentityAndUser(t *testing.T) {
	s, _, actor, removed, _, _ := identityFixture(t)
	if _, err := s.Store.DB.Exec("CREATE TRIGGER reject_identity_audit BEFORE INSERT ON audit_events WHEN NEW.action='user.identity.unbind' BEGIN SELECT RAISE(ABORT,'private-identity-detail'); END"); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	if err := s.UnbindIdentity(context.Background(), actor, removed.Session.UserID, removed.Session.IdentityID); err == nil {
		t.Fatal("audit failure ignored")
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("missing unbind diagnostics: %s %v", logs.String(), err)
	}
	if entry["user_id"] != removed.Session.UserID || entry["identity_id"] != removed.Session.IdentityID || entry["stage"] != "audit" || entry["reason"] != "sqlite_1811" || strings.Contains(logs.String(), "private-identity-detail") {
		t.Fatal(entry)
	}
}

func TestExecuteRollbackLogsOperationTargetAndFailurePhase(t *testing.T) {
	s, q, actor, _ := taskFixture(t)
	_, orderID := taskOrder(t, s, q, "geo", "automatic", "target")
	ctx := context.Background()
	op, err := s.Preview(ctx, actor, "hide_order", []string{orderID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.Exec("CREATE TRIGGER reject_operation_audit BEFORE INSERT ON audit_events WHEN NEW.action='order.hide_order' BEGIN SELECT RAISE(ABORT,'private-audit-detail'); END"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&output, nil))
	if _, err = s.Execute(ctx, actor, op.ID); err == nil || !strings.Contains(err.Error(), "private-audit-detail") {
		t.Fatal(err)
	}
	var entry map[string]any
	if err = json.Unmarshal(bytes.TrimSpace(output.Bytes()), &entry); err != nil {
		t.Fatalf("missing rollback failure log: %s (%v)", output.String(), err)
	}
	if entry["operation_id"] != op.ID || entry["actor_id"] != actor || entry["order_id"] != orderID || entry["stage"] != "audit" || entry["action"] != "hide_order" || entry["reason"] != "sqlite_1811" {
		t.Fatal(entry)
	}
	message, _ := entry["msg"].(string)
	if !strings.Contains(message, op.ID) || !strings.Contains(message, orderID) || entry["duration_ms"] == nil || strings.Contains(output.String(), "private-audit-detail") {
		t.Fatal(entry)
	}
	o, err := s.Orders.Get(ctx, orderID)
	if err != nil || o.HiddenOrder {
		t.Fatal("failed operation did not roll back", o, err)
	}
}
