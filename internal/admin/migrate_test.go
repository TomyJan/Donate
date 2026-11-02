package admin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func migrationFixture(t *testing.T) (*Service, *auth.Service, string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{}, logger)
	users := make([]string, 3)
	for i, subject := range []string{"actor", "target", "other"} {
		c, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "migration", Subject: subject, Nickname: subject})
		if err != nil {
			t.Fatal(err)
		}
		users[i] = c.Session.UserID
	}
	s := New(db, order.New(db, logger, "g", "p", "v2"), config.Config{AdminIDs: map[string]bool{users[0]: true}}, logger)
	id, loc := storage.NewID(), storage.NewID()
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO orders(id,merchant_order_no,amount_fen,channel,message,status,created_at,expires_at,paid_at,protocol,request_ip,donor_nickname,donor_avatar_url,donor_qq_number,trade_no,api_trade_no,gateway_status,last_checked_at,hidden_message,version) VALUES(?,'original-source',888,'alipay','original message','paid',1640995200,1640997000,1640995300,'legacy','192.0.2.1','历史昵称','https://example.test/original.png','123456','original-trade','original-api-trade','paid',1640995400,1,7)`, id[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,province,source,source_version,queried_at) VALUES(?,?,'原始省份','legacy','original-version',1640995500)", loc[:], id[:]); err != nil {
		t.Fatal(err)
	}
	observation := storage.NewID()
	if _, err = db.DB.ExecContext(ctx, "INSERT INTO payment_observations(id,order_id,source,request_id,trade_no,status,amount_fen,observed_at,result) VALUES(?,?,'legacy','original-request','original-trade','paid',888,1640995500,'paid')", observation[:], id[:]); err != nil {
		t.Fatal(err)
	}
	return s, a, users[0], users[1], users[2], id.String()
}

func migrationRow(t *testing.T, db *sql.DB, query string, args ...any) map[string]any {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("expected stored row")
	}
	values, pointers := make([]any, len(columns)), make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]any, len(columns))
	for i, name := range columns {
		out[name] = values[i]
	}
	return out
}

// Rewriting historical snapshots, modifying payment/location data, omitting the
// version increment, or auditing a replay must all fail this integration test.
func TestMigrateLegacyOrderPreservesHistoryAndUpdatesOwnershipViews(t *testing.T) {
	s, _, actor, target, _, id := migrationFixture(t)
	ctx := context.Background()
	oid, _ := storage.ParseID(id)
	before := migrationRow(t, s.Store.DB, "SELECT * FROM orders WHERE id=?", oid)
	location := migrationRow(t, s.Store.DB, "SELECT * FROM order_locations WHERE order_id=?", oid)
	observation := migrationRow(t, s.Store.DB, "SELECT * FROM payment_observations WHERE order_id=?", oid)
	for index, spelling := range []string{strings.ToUpper(target), strings.ReplaceAll(target, "-", "")} {
		got, err := s.MigrateLegacyOrder(ctx, actor, strings.ToUpper(id), spelling)
		want := "migrated"
		if index == 1 {
			want = "unchanged"
		}
		if err != nil || got.ID != id || got.UserID != target || got.Result != want {
			t.Fatalf("result %+v, error %v", got, err)
		}
	}
	after := migrationRow(t, s.Store.DB, "SELECT * FROM orders WHERE id=?", oid)
	targetBytes, _ := storage.ParseID(target)
	if !bytes.Equal(after["user_id"].([]byte), targetBytes) || after["version"] != int64(8) {
		t.Fatalf("ownership/version: %+v", after)
	}
	delete(before, "user_id")
	delete(after, "user_id")
	delete(before, "version")
	delete(after, "version")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("historical order changed: before %+v, after %+v", before, after)
	}
	if !reflect.DeepEqual(location, migrationRow(t, s.Store.DB, "SELECT * FROM order_locations WHERE order_id=?", oid)) || !reflect.DeepEqual(observation, migrationRow(t, s.Store.DB, "SELECT * FROM payment_observations WHERE order_id=?", oid)) {
		t.Fatal("migration rewrote historical location/payment evidence")
	}
	var count int
	var result, action, targetType string
	var auditActor, auditOrder []byte
	if err := s.Store.DB.QueryRow("SELECT count(*),action,actor_id,target_type,target_id,result FROM audit_events WHERE action='order.migrate'").Scan(&count, &action, &auditActor, &targetType, &auditOrder, &result); err != nil {
		t.Fatal(err)
	}
	if count != 1 || action != "order.migrate" || storage.IDString(auditActor) != actor || targetType != "order" || storage.IDString(auditOrder) != id || result != "migrated_to:"+target {
		t.Fatalf("migration audit count %d actor %s target %s/%s result %s", count, storage.IDString(auditActor), targetType, storage.IDString(auditOrder), result)
	}
	mine, err := s.Orders.List(ctx, target, true, "")
	if err != nil || len(mine.Items) != 1 || mine.Items[0].ID != id || !mine.Items[0].IsMine || mine.Items[0].Nickname != "target" || mine.Items[0].Province != "原始省份" || mine.Items[0].Message != "original message" {
		t.Fatalf("mine: %+v %v", mine, err)
	}
	public, err := s.Orders.List(ctx, "", false, "")
	if err != nil || len(public.Items) != 1 || public.Items[0].Nickname != "target" || public.Items[0].AvatarURL != "" || public.Items[0].Message != "" {
		t.Fatalf("public: %+v %v", public, err)
	}
	user, err := s.User(ctx, target)
	if err != nil || user.OrderCount != 1 || user.PaidFen != 888 {
		t.Fatalf("user aggregates: %+v %v", user, err)
	}
	filtered, err := s.OrdersList(ctx, Filter{UserID: target, Size: 10})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != id {
		t.Fatalf("admin user orders: %+v %v", filtered, err)
	}
}

func TestMigrateLegacyOrderRejectsIneligibleAndMissingRecords(t *testing.T) {
	for _, tc := range []struct {
		name, change                                                string
		useOther, missingOrder, missingUser, invalidID, invalidUser bool
		want                                                        error
	}{
		{name: "ordinary order", change: "UPDATE orders SET protocol='v2'", want: ErrOrderMigrationNotAllowed},
		{name: "ordinary already assigned same user", change: "UPDATE orders SET protocol='v2',user_id=?", want: ErrOrderMigrationNotAllowed},
		{name: "different legacy owner", change: "UPDATE orders SET user_id=?", useOther: true, want: ErrOrderMigrationNotAllowed},
		{name: "missing order", missingOrder: true, want: order.ErrNotFound},
		{name: "missing user", missingUser: true, want: order.ErrNotFound},
		{name: "invalid order", invalidID: true, want: auth.ErrInvalid},
		{name: "invalid user", invalidUser: true, want: auth.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, actor, target, other, id := migrationFixture(t)
			if tc.change != "" {
				args := []any{}
				if strings.Contains(tc.change, "?") {
					user := target
					if tc.useOther {
						user = other
					}
					uid, _ := storage.ParseID(user)
					args = append(args, uid)
				}
				if _, err := s.Store.DB.Exec(tc.change, args...); err != nil {
					t.Fatal(err)
				}
			}
			before := migrationRow(t, s.Store.DB, "SELECT * FROM orders")
			if tc.missingOrder {
				id = storage.NewID().String()
			}
			if tc.missingUser {
				target = storage.NewID().String()
			}
			if tc.invalidID {
				id = "bad"
			}
			if tc.invalidUser {
				target = "bad"
			}
			if _, err := s.MigrateLegacyOrder(context.Background(), actor, id, target); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if !reflect.DeepEqual(before, migrationRow(t, s.Store.DB, "SELECT * FROM orders")) {
				t.Fatal("rejected migration changed order")
			}
			var n int
			if err := s.Store.DB.QueryRow("SELECT count(*) FROM audit_events WHERE action='order.migrate'").Scan(&n); err != nil || n != 0 {
				t.Fatalf("audit %d %v", n, err)
			}
		})
	}
}

func TestMigrateLegacyOrderChecksCurrentActorAndAllowsBannedRecipient(t *testing.T) {
	for _, mode := range []string{"nonadmin", "revoked admin", "banned actor", "banned recipient"} {
		t.Run(mode, func(t *testing.T) {
			s, a, actor, target, _, id := migrationFixture(t)
			ctx := context.Background()
			want := auth.ErrUnauthorized
			switch mode {
			case "nonadmin":
				actor = target
			case "revoked admin":
				delete(s.Config.AdminIDs, actor)
			case "banned actor":
				uid, _ := storage.ParseID(actor)
				if _, err := s.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", uid); err != nil {
					t.Fatal(err)
				}
				want = auth.ErrBanned
			case "banned recipient":
				if err := s.Ban(ctx, actor, target, true); err != nil {
					t.Fatal(err)
				}
				want = nil
			}
			got, err := s.MigrateLegacyOrder(ctx, actor, id, target)
			if !errors.Is(err, want) {
				t.Fatalf("result %+v error %v, want %v", got, err, want)
			}
			if mode == "banned recipient" {
				if got.Result != "migrated" {
					t.Fatal(got)
				}
				if _, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "migration", Subject: "target"}); !errors.Is(err, auth.ErrBanned) {
					t.Fatalf("recipient was unbanned: %v", err)
				}
			} else {
				o, err := s.Orders.Get(ctx, id)
				if err != nil || o.UserID != "" {
					t.Fatalf("unauthorized migration %+v %v", o, err)
				}
			}
		})
	}
}

func TestConcurrentMigrationsCannotReassignOrder(t *testing.T) {
	s, _, actor, first, second, id := migrationFixture(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, target := range []string{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.MigrateLegacyOrder(context.Background(), actor, id, target)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	succeeded, rejected := 0, 0
	for err := range errs {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrOrderMigrationNotAllowed) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("successes %d rejections %d", succeeded, rejected)
	}
	o, err := s.Orders.Get(context.Background(), id)
	if err != nil || (o.UserID != first && o.UserID != second) {
		t.Fatalf("owner %+v %v", o, err)
	}
	var n, version int
	oid, _ := storage.ParseID(id)
	if err = s.Store.DB.QueryRow("SELECT version,(SELECT count(*) FROM audit_events WHERE action='order.migrate') FROM orders WHERE id=?", oid).Scan(&version, &n); err != nil || version != 8 || n != 1 {
		t.Fatalf("version %d audit %d error %v", version, n, err)
	}
}

func TestMigrateLegacyOrderRollsBackOwnershipWhenAuditFails(t *testing.T) {
	s, _, actor, target, _, id := migrationFixture(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := s.Store.DB.Exec("CREATE TRIGGER reject_migration BEFORE INSERT ON audit_events WHEN NEW.action='order.migrate' BEGIN SELECT RAISE(ABORT,'private-sql-content'); END"); err != nil {
		t.Fatal(err)
	}
	before := migrationRow(t, s.Store.DB, "SELECT * FROM orders")
	if _, err := s.MigrateLegacyOrder(context.Background(), actor, id, target); err == nil {
		t.Fatal("audit failure accepted")
	}
	if !reflect.DeepEqual(before, migrationRow(t, s.Store.DB, "SELECT * FROM orders")) {
		t.Fatal("order assignment escaped audit rollback")
	}
	if strings.Contains(logs.String(), "private-sql-content") || !strings.Contains(logs.String(), "stage=audit") || !strings.Contains(logs.String(), "order_id="+id) || !strings.Contains(logs.String(), "user_id="+target) {
		t.Fatalf("unsafe/incomplete failure diagnostics: %s", logs.String())
	}
	if _, err := s.Store.DB.Exec("DROP TRIGGER reject_migration"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.MigrateLegacyOrder(context.Background(), actor, id, target); err != nil || got.Result != "migrated" {
		t.Fatalf("retry after rollback: %+v %v", got, err)
	}
}

func TestMigrateLegacyOrderLogsBusinessRejectionWithControlledReason(t *testing.T) {
	s, _, actor, target, _, id := migrationFixture(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := s.Store.DB.Exec("UPDATE orders SET protocol='v2'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MigrateLegacyOrder(context.Background(), actor, id, target); !errors.Is(err, ErrOrderMigrationNotAllowed) {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "reason=order_migration_not_allowed") || !strings.Contains(logs.String(), "actor_id="+actor) || !strings.Contains(logs.String(), "order_id="+id) || !strings.Contains(logs.String(), "user_id="+target) {
		t.Fatalf("business rejection was not classified: %s", logs.String())
	}
}

func TestMigrateLegacyOrderUsesLiveRecipientIdentityAcrossViews(t *testing.T) {
	for _, tc := range []struct{ name, nickname, display string }{
		{"empty snapshots", "", "捐赠者"},
		{"whitespace nickname", "\t　\r\n", "捐赠者"},
		{"missing avatar only", "Original history", "Original history"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, actor, target, _, id := migrationFixture(t)
			ctx := context.Background()
			oid, _ := storage.ParseID(id)
			uid, _ := storage.ParseID(target)
			if _, err := s.Store.DB.Exec("UPDATE identities SET nickname='Recipient Z',avatar_url='https://example.test/recipient.png' WHERE user_id=?", uid); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.DB.Exec("UPDATE orders SET donor_nickname=?,donor_avatar_url='' WHERE id=?", tc.nickname, oid); err != nil {
				t.Fatal(err)
			}
			named, job := storage.NewID(), storage.NewID()
			if _, err := s.Store.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,protocol,request_ip,donor_nickname) VALUES(?,'alpha-history',50,'qqpay','paid',1640995200,1640997000,'legacy','192.0.2.2','Alpha history')", named[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.DB.Exec("INSERT INTO jobs(id,kind,order_id,run_at,created_at,updated_at) VALUES(?,'geo',?,1640995200,1640995200,1640995200)", job[:], oid); err != nil {
				t.Fatal(err)
			}
			// A normal account order must continue to use its owner's profile.
			native, err := s.Orders.Create(ctx, order.CreateInput{UserID: target, AmountFen: 9, Channel: "alipay", Key: "native-profile"})
			if err != nil {
				t.Fatal(err)
			}
			aid, _ := storage.ParseID(actor)
			if err := s.Store.Write(ctx, func(tx *sql.Tx) error {
				for _, orderID := range [][]byte{oid, named[:]} {
					if err := storage.Audit(ctx, tx, aid, "order.snapshot", "order", orderID, "original", 1640995200); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			wantSorted := []string{id, named.String(), native.ID}
			wantAudit := []string{id, named.String()}
			wantAuditNickname, wantAuditLabel := "", "¥8.88"
			if tc.nickname == "Original history" {
				wantSorted = []string{named.String(), id, native.ID}
				wantAudit = []string{named.String(), id}
				wantAuditNickname, wantAuditLabel = "Original history", "Original history · ¥8.88"
			}
			recipientName, recipientAvatar := "Recipient Z", "https://example.test/recipient.png"
			checkViews := func(stage string) {
				t.Helper()
				owned := stage != "before"
				wantNickname, wantAvatar := tc.display, ""
				if owned {
					wantNickname, wantAvatar = recipientName, recipientAvatar
					wantSorted = []string{named.String(), id, native.ID}
					if id > native.ID {
						wantSorted[1], wantSorted[2] = wantSorted[2], wantSorted[1]
					}
					wantAudit = []string{named.String(), id}
					wantAuditNickname, wantAuditLabel = recipientName, recipientName+" · ¥8.88"
				}
				public, err := s.Orders.List(ctx, "", false, "")
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range public.Items {
					if item.ID == id && (item.Nickname != wantNickname || item.AvatarURL != wantAvatar) {
						t.Errorf("%s public adopted recipient: %+v", stage, item)
					}
				}
				for _, full := range []bool{false, true} {
					detail, err := s.Detail(ctx, id, full)
					if err != nil || detail.Nickname != wantNickname || detail.AvatarURL != wantAvatar {
						t.Errorf("%s detail full=%v: %+v %v", stage, full, detail, err)
					}
				}
				page, err := s.OrdersList(ctx, Filter{Sort: "nickname", Direction: "asc", Size: 10})
				if err != nil {
					t.Fatal(err)
				}
				var sorted []string
				for _, item := range page.Items {
					sorted = append(sorted, item.ID)
					if item.ID == id && (item.Nickname != wantNickname || item.AvatarURL != wantAvatar) {
						t.Errorf("%s admin adopted recipient: %+v", stage, item)
					}
					if item.ID == native.ID && (item.Nickname != recipientName || item.AvatarURL != recipientAvatar) {
						t.Errorf("%s native profile changed: %+v", stage, item)
					}
				}
				if !reflect.DeepEqual(sorted, wantSorted) {
					t.Errorf("%s nickname sort=%v, want %v", stage, sorted, wantSorted)
				}
				search, err := s.OrdersList(ctx, Filter{Search: recipientName, Size: 10})
				wantTotal := 1
				if owned {
					wantTotal = 2
				}
				if err != nil || search.Total != wantTotal || len(search.Items) != wantTotal {
					t.Errorf("%s legacy matched recipient nickname: %+v %v", stage, search, err)
				}
				audit, err := s.Audit(ctx, Filter{Action: "order.snapshot", Sort: "target", Direction: "asc", Size: 10})
				if err != nil {
					t.Fatal(err)
				}
				var auditOrder []string
				for _, item := range audit.Items {
					auditOrder = append(auditOrder, item.TargetID)
					if item.TargetID == id && (item.TargetNickname != wantAuditNickname || item.Target != wantAuditLabel) {
						t.Errorf("%s audit adopted recipient: %+v", stage, item)
					}
				}
				if !reflect.DeepEqual(auditOrder, wantAudit) {
					t.Errorf("%s audit target sort=%v, want %v", stage, auditOrder, wantAudit)
				}
				task, err := s.Task(ctx, job.String())
				wantTask := tc.nickname
				if wantTask == "" {
					wantTask = "捐赠者"
				}
				if owned {
					wantTask = recipientName
				}
				if err != nil || task.Nickname != wantTask {
					t.Errorf("%s task adopted recipient: %+v %v", stage, task, err)
				}
				tasks, err := s.Tasks(ctx, TaskFilter{Search: recipientName, Size: 10})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, item := range tasks.Items {
					if item.OrderID == id {
						found = true
					}
				}
				if found != owned {
					t.Errorf("%s task recipient search found=%v", stage, found)
				}
			}
			checkViews("before")
			if _, err := s.MigrateLegacyOrder(ctx, actor, id, target); err != nil {
				t.Fatal(err)
			}
			checkViews("after")
			recipientName, recipientAvatar = "Recipient updated", "https://example.test/updated.png"
			if _, err := s.Store.DB.Exec("UPDATE identities SET nickname=?,avatar_url=? WHERE user_id=?", recipientName, recipientAvatar, uid); err != nil {
				t.Fatal(err)
			}
			checkViews("profile updated")
			recipientAvatar = ""
			if _, err := s.Store.DB.Exec("UPDATE identities SET avatar_url='' WHERE user_id=?", uid); err != nil {
				t.Fatal(err)
			}
			checkViews("avatar removed")
			stored, err := s.Orders.Get(ctx, id)
			if err != nil || stored.DonorNickname != tc.nickname || stored.DonorAvatarURL != "" {
				t.Fatalf("raw snapshots rewritten: %+v %v", stored, err)
			}
		})
	}
}

func TestMigratedOrderUsesCurrentQQAndNeverReusesImportedQQ(t *testing.T) {
	s, a, actor, _, _, id := migrationFixture(t)
	ctx := context.Background()
	recipient, err := a.LoginIdentity(ctx, auth.Identity{Provider: "qq", Issuer: "migration", Subject: "qq-recipient", Nickname: "QQ recipient", QQNumber: "87654321"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MigrateLegacyOrder(ctx, actor, id, recipient.Session.UserID); err != nil {
		t.Fatal(err)
	}
	uid, _ := storage.ParseID(recipient.Session.UserID)
	for _, number := range []string{"87654321", "99887766", ""} {
		if _, err = s.Store.DB.Exec("UPDATE identities SET qq_number=? WHERE user_id=? AND provider='qq'", number, uid); err != nil {
			t.Fatal(err)
		}
		full, err := s.Detail(ctx, id, true)
		if err != nil || full.QQNumber != number {
			t.Fatalf("current QQ: %q want %q, %v", full.QQNumber, number, err)
		}
		owned, err := s.Detail(ctx, id, false)
		if err != nil || owned.QQNumber != "" {
			t.Fatalf("ordinary detail exposed QQ: %+v %v", owned, err)
		}
	}
}
