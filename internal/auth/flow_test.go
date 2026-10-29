package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFlowRejectsStateReplayAndWrongBrowser(t *testing.T) {
	s := testAuth(t)
	s.Config.QQappID = "app"
	s.Config.QQsecret = "secret"
	s.Config.PublicURL = "https://donate.test"
	flow, e := s.BeginFlow(context.Background(), "qq", "login", nil, "/")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.claimFlow(context.Background(), flow.State, "wrong"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("wrong browser accepted", e)
	}
	if _, e = s.claimFlow(context.Background(), flow.State, flow.Cookie); e != nil {
		t.Fatal(e)
	}
	if _, e = s.claimFlow(context.Background(), flow.State, flow.Cookie); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("replay accepted", e)
	}
}
func TestFlowExpires(t *testing.T) {
	s := testAuth(t)
	s.Config.QQappID = "app"
	s.Config.PublicURL = "https://donate.test"
	now := time.Now()
	s.Now = func() time.Time { return now }
	f, e := s.BeginFlow(context.Background(), "qq", "login", nil, "/")
	if e != nil {
		t.Fatal(e)
	}
	now = now.Add(11 * time.Minute)
	if _, e = s.claimFlow(context.Background(), f.State, f.Cookie); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
}
