package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func identityFixture(t *testing.T) (*Service, *auth.Service, string, auth.Credentials, auth.Credentials, *bytes.Buffer) {
	t.Helper()
	s, actor := readFixture(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logs, nil))
	cfg := s.Config
	cfg.Secret = make([]byte, 32)
	cfg.QQappID = "test-app"
	a := auth.New(s.Store, cfg, s.Log)
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	removed, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "target", Nickname: "OIDC target"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	qq := auth.Identity{Provider: "qq", Issuer: "test-app", Subject: "target-qq", Nickname: "QQ target", QQNumber: "123456"}
	if err = a.BindIdentity(context.Background(), removed.Session.ID, qq); err != nil {
		t.Fatal(err)
	}
	retained, err := a.LoginIdentity(context.Background(), qq)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise replacement of the public identity, with several sessions on it.
	if _, err = a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "target", Nickname: "OIDC target"}); err != nil {
		t.Fatal(err)
	}
	return s, a, actor, removed, retained, &logs
}

func TestUnbindIdentityPreservesAccountAndOtherLoginMethod(t *testing.T) {
	s, a, actor, removed, retained, logs := identityFixture(t)
	ctx := context.Background()
	o, err := s.Orders.Create(ctx, order.CreateInput{UserID: removed.Session.UserID, Key: "retained-order", AmountFen: 123, Channel: "alipay"})
	if err != nil {
		t.Fatal(err)
	}
	var flows []auth.FlowStart
	for i, session := range []*auth.Session{&removed.Session, &retained.Session, &retained.Session} {
		f, err := a.BeginFlow(ctx, "qq", "bind", session, "/admin")
		if err != nil {
			t.Fatal(err)
		}
		flows = append(flows, f)
		stage := []string{"pending", "processing", "finishing"}[i]
		if _, err = s.Store.DB.ExecContext(ctx, "UPDATE auth_flows SET stage=? WHERE intent='bind' AND stage='pending'", stage); err != nil {
			t.Fatal(err)
		}
	}
	loginFlow, err := a.BeginFlow(ctx, "qq", "login", &removed.Session, "/")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UnbindIdentity(ctx, actor, removed.Session.UserID, removed.Session.IdentityID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Resolve(ctx, removed.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("removed credential usable: %v", err)
	}
	if _, err = a.Renew(ctx, removed.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("removed credential renewed: %v", err)
	}
	if _, err = a.Resolve(ctx, retained.Token); err != nil {
		t.Fatalf("remaining credential lost: %v", err)
	}
	for _, f := range append(flows, loginFlow) {
		if _, err = a.FlowStatus(ctx, f.Cookie); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatalf("authorization flow survived: %v", err)
		}
	}
	u, err := s.User(ctx, removed.Session.UserID)
	if err != nil || len(u.Bindings) != 1 || u.Bindings[0].ID != retained.Session.IdentityID || u.Nickname != "QQ target" {
		t.Fatalf("remaining user=%+v err=%v", u, err)
	}
	if _, err = s.Orders.Get(ctx, o.ID); err != nil {
		t.Fatalf("order lost: %v", err)
	}
	uid, _ := storage.ParseID(removed.Session.UserID)
	iid, _ := storage.ParseID(removed.Session.IdentityID)
	var sessions, audit int
	if err = s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE identity_id=?", iid).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("identity sessions=%d err=%v", sessions, err)
	}
	if err = s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM audit_events WHERE action='user.identity.unbind' AND target_type='user' AND target_id=?", uid).Scan(&audit); err != nil || audit != 1 {
		t.Fatalf("audit=%d err=%v", audit, err)
	}
	if _, err = a.LoginIdentity(ctx, auth.Identity{Provider: "qq", Issuer: "test-app", Subject: "target-qq", QQNumber: "123456"}); err != nil {
		t.Fatalf("remaining login failed: %v", err)
	}
	for _, id := range []string{actor, removed.Session.UserID, removed.Session.IdentityID} {
		if !bytes.Contains(logs.Bytes(), []byte(id)) {
			t.Fatalf("missing unique ID in log: %s", id)
		}
	}
	if bytes.Contains(logs.Bytes(), []byte("target-qq")) || bytes.Contains(logs.Bytes(), []byte("123456")) || bytes.Contains(logs.Bytes(), []byte(removed.Token)) {
		t.Fatal("secret leaked in log")
	}
}

func TestUnbindIdentityRejectsLastMethodAndTargetMismatch(t *testing.T) {
	s, a, actor, removed, retained, _ := identityFixture(t)
	ctx := context.Background()
	if err := s.UnbindIdentity(ctx, actor, actor, removed.Session.IdentityID); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("mismatched target=%v", err)
	}
	if _, err := a.Resolve(ctx, removed.Token); err != nil {
		t.Fatal("mismatch revoked target", err)
	}
	if err := s.UnbindIdentity(ctx, removed.Session.UserID, removed.Session.UserID, removed.Session.IdentityID); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("member authorization=%v", err)
	}
	if err := s.UnbindIdentity(ctx, actor, "bad-id", removed.Session.IdentityID); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("bad UUID=%v", err)
	}
	if err := s.UnbindIdentity(ctx, actor, removed.Session.UserID, removed.Session.IdentityID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnbindIdentity(ctx, actor, retained.Session.UserID, retained.Session.IdentityID); !errors.Is(err, ErrLastIdentity) {
		t.Fatalf("last identity=%v", err)
	}
	if _, err := a.Resolve(ctx, retained.Token); err != nil {
		t.Fatal("last-identity refusal changed credential", err)
	}
}

func TestConcurrentIdentityUnbindKeepsOneMethod(t *testing.T) {
	s, _, actor, removed, retained, _ := identityFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{removed.Session.IdentityID, retained.Session.IdentityID} {
		wg.Go(func() { <-start; results <- s.UnbindIdentity(context.Background(), actor, removed.Session.UserID, id) })
	}
	close(start)
	wg.Wait()
	close(results)
	var success, forbidden int
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrLastIdentity) {
			forbidden++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || forbidden != 1 {
		t.Fatalf("success=%d forbidden=%d", success, forbidden)
	}
	u, err := s.User(context.Background(), removed.Session.UserID)
	if err != nil || len(u.Bindings) != 1 || u.Nickname == "" {
		t.Fatalf("user=%+v err=%v", u, err)
	}
}

func TestBindingTimestampsComeFromCreationAndHideSecrets(t *testing.T) {
	s, a, _, removed, _, _ := identityFixture(t)
	ctx := context.Background()
	p, err := a.Profile(ctx, removed.Session)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.User(ctx, removed.Session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, bindings := range [][]auth.Identity{p.Bindings, u.Bindings} {
		if len(bindings) != 2 {
			t.Fatal(bindings)
		}
		for _, b := range bindings {
			want := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
			if b.Provider == "qq" {
				want = want.Add(time.Minute)
			}
			if !b.BoundAt.Equal(want) {
				t.Fatalf("bound timestamp changed on login: %+v", b)
			}
			if _, err = storage.ParseID(b.ID); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err = json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["bound_at"] == nil || len(fields) != 5 {
				t.Fatalf("binding fields=%s", raw)
			}
		}
	}
}

func TestUnbindIdentityRollsBackIfAuditFails(t *testing.T) {
	s, a, actor, removed, _, _ := identityFixture(t)
	ctx := context.Background()
	if _, err := s.Store.DB.ExecContext(ctx, "CREATE TRIGGER reject_unbind_audit BEFORE INSERT ON audit_events WHEN NEW.action='user.identity.unbind' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnbindIdentity(ctx, actor, removed.Session.UserID, removed.Session.IdentityID); err == nil {
		t.Fatal("audit failure ignored")
	}
	if _, err := a.Resolve(ctx, removed.Token); err != nil {
		t.Fatal("rollback lost session", err)
	}
	u, err := s.User(ctx, removed.Session.UserID)
	if err != nil || len(u.Bindings) != 2 || u.Nickname != "OIDC target" {
		t.Fatalf("rollback user=%+v err=%v", u, err)
	}
}

type identityRoundTrip func(*http.Request) (*http.Response, error)

func (fn identityRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestUnbindCancelsBindingCallbackAlreadyExchangingCredentials(t *testing.T) {
	s, a, actor, retained, removed, _ := identityFixture(t)
	ctx := context.Background()
	f, err := a.BeginFlow(ctx, "qq", "bind", &retained.Session, "/admin")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a.HTTP = &http.Client{Transport: identityRoundTrip(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/oauth2.0/token":
			close(started)
			<-release
			body = `{"access_token":"private-test-token"}`
		case "/oauth2.0/me":
			body = `{"client_id":"test-app","openid":"target-qq"}`
		case "/user/get_user_info":
			body = `{"ret":0,"nickname":"QQ target"}`
		default:
			return nil, errors.New("unexpected OAuth request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	result := make(chan error, 1)
	go func() {
		_, err := a.Callback(ctx, "qq", f.State, "test-code", f.Cookie, &retained.Session)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("callback exchange did not start")
	}
	if err = s.UnbindIdentity(ctx, actor, removed.Session.UserID, removed.Session.IdentityID); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case err = <-result:
		if !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatalf("revoked binding callback accepted: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not finish")
	}
	u, err := s.User(ctx, retained.Session.UserID)
	if err != nil || len(u.Bindings) != 1 || u.Bindings[0].ID != retained.Session.IdentityID {
		t.Fatalf("callback rebound identity: %+v err=%v", u, err)
	}
	if _, err = a.Resolve(ctx, retained.Token); err != nil {
		t.Fatal("remaining session revoked", err)
	}
}
