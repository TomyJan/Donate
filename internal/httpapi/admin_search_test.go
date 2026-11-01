package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
)

func adminSearchResponse(h http.Handler, s *Server, administrator auth.Credentials, resource, search string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/"+resource+"/list?search="+url.QueryEscape(search), nil)
	r.AddCookie(&http.Cookie{Name: s.sessionCookieName(administrator.Session.ID), Value: administrator.Token})
	r.Header.Set("X-Session-Scope", administrator.Session.CSRF)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAdminSearchFindsCompleteLegalUnicodeNicknames(t *testing.T) {
	for _, tc := range []struct{ name, nickname string }{
		{"ASCII80", strings.Repeat("A", 80)},
		{"Han42", strings.Repeat("中", 42)},
		{"Han43", strings.Repeat("中", 43)},
		{"Han80", strings.Repeat("中", 80)},
		{"Emoji33", strings.Repeat("😀", 33)},
		{"Emoji80", strings.Repeat("😀", 80)},
		{"Accent65", strings.Repeat("É", 65)},
		{"Accent80", strings.Repeat("É", 80)},
		{"LiteralPatterns80", strings.Repeat("中", 75) + "\\_%ÉZ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, a := setup(t)
			ctx := context.Background()
			administrator, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "long-search", Subject: "admin", Nickname: "Administrator"})
			if err != nil {
				t.Fatal(err)
			}
			s.Config.AdminIDs[administrator.Session.UserID] = true
			var target order.Order
			var userID string
			runes := []rune(tc.nickname)
			distractor := string(runes[:len(runes)-1]) + "X"
			for i, nickname := range []string{tc.nickname, distractor} {
				owner, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "long-search", Subject: string(rune('a' + i)), Nickname: nickname})
				if err != nil {
					t.Fatal(err)
				}
				profile, err := a.Profile(ctx, owner.Session)
				if err != nil || profile.Nickname != nickname {
					t.Fatalf("legal nickname not preserved: got=%q want=%q err=%v", profile.Nickname, nickname, err)
				}
				created, err := s.Orders.Create(ctx, order.CreateInput{UserID: owner.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "long-search"})
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					target, userID = created, owner.Session.UserID
				}
			}
			for _, resource := range []struct {
				name string
				want int
			}{{"user", 1}, {"order", 1}, {"task", 2}} {
				w := adminSearchResponse(h, s, administrator, resource.name, tc.nickname)
				var body struct {
					Code string
					Data struct {
						Total int
						Items []map[string]any
					}
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusOK || body.Code != "OK" || body.Data.Total != resource.want || len(body.Data.Items) != resource.want {
					t.Errorf("%s exact nickname search (%d runes/%d bytes): status=%d body=%s", resource.name, utf8.RuneCountInString(tc.nickname), len(tc.nickname), w.Code, w.Body)
					continue
				}
				for _, item := range body.Data.Items {
					key, want := "id", userID
					if resource.name == "order" {
						want = target.ID
					} else if resource.name == "task" {
						key, want = "order_id", target.ID
					}
					if item[key] != want || item["nickname"] != tc.nickname {
						t.Errorf("%s search selected another owner: %+v", resource.name, item)
					}
				}
			}
		})
	}
}

func TestAdminSearchEnforcesUnicodeCodepointAndUTF8Bounds(t *testing.T) {
	s, h, a := setup(t)
	administrator, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "search-bounds", Subject: "admin", Nickname: "Administrator"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[administrator.Session.UserID] = true
	for _, tc := range []struct {
		name, search string
		valid        bool
	}{
		{"ASCII128", strings.Repeat("a", 128), true},
		{"Han128", strings.Repeat("中", 128), true},
		{"Emoji128", strings.Repeat("😀", 128), true},
		{"Combining128", strings.Repeat("e\u0301", 64), true},
		{"LiteralPatterns128", strings.Repeat("%_", 64), true},
		{"ReplacementCharacter", "\ufffd", true},
		{"ASCII129", strings.Repeat("a", 129), false},
		{"Han129", strings.Repeat("中", 129), false},
		{"Emoji129", strings.Repeat("😀", 129), false},
		{"InvalidByte", string([]byte{0xff}), false},
		{"TruncatedUTF8", string([]byte{0xe4, 0xb8}), false},
		{"OverlongUTF8", string([]byte{0xc0, 0xaf}), false},
		{"SurrogateUTF8", string([]byte{0xed, 0xa0, 0x80}), false},
		{"OutOfRangeUTF8", string([]byte{0xf4, 0x90, 0x80, 0x80}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, resource := range []string{"user", "order", "task"} {
				w := adminSearchResponse(h, s, administrator, resource, tc.search)
				var body struct {
					Code string
					Data struct {
						Total int
						Items []json.RawMessage
					}
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if tc.valid {
					if w.Code != 200 || body.Code != "OK" || body.Data.Total != 0 || len(body.Data.Items) != 0 {
						t.Errorf("%s valid bound rejected: status=%d body=%s", resource, w.Code, w.Body)
					}
				} else if w.Code != http.StatusOK || body.Code != "VALIDATION_FAILED" {
					t.Errorf("%s invalid search accepted: status=%d body=%s", resource, w.Code, w.Body)
				}
			}
		})
	}
}

func TestAdminSearchRejectsNULWithoutBroadeningResults(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	owner, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "nul-search", Subject: "owner", Nickname: "Unique Normal Owner"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[owner.Session.UserID] = true
	if _, err = s.Orders.Create(ctx, order.CreateInput{UserID: owner.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "nul-search"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, search   string
		valid, matches bool
	}{
		{"empty", "", true, true},
		{"ordinary", "Unique Normal Owner", true, true},
		{"absent", "DefinitelyAbsentName", true, false},
		{"literal-patterns", "Unique\\_%Owner", true, false},
		{"other-control-characters", "\t\nDefinitelyAbsentName\r", true, false},
		{"leading-ordinary", "\x00Unique Normal Owner", false, false},
		{"middle-ordinary", "Unique Normal\x00 Owner", false, false},
		{"trailing-ordinary", "Unique Normal Owner\x00", false, false},
		{"leading-absent", "\x00DefinitelyAbsentName", false, false},
		{"middle-absent", "Definitely\x00AbsentName", false, false},
		{"trailing-absent", "DefinitelyAbsentName\x00", false, false},
		{"leading-patterns", "\x00Unique\\_%Owner", false, false},
		{"middle-patterns", "Unique\\_\x00%Owner", false, false},
		{"trailing-patterns", "Unique\\_%Owner\x00", false, false},
		{"nul-only", "\x00", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, resource := range []struct {
				name string
				rows int
			}{{"user", 1}, {"order", 1}, {"task", 2}} {
				w := adminSearchResponse(h, s, owner, resource.name, tc.search)
				var body struct {
					Code string
					Data struct {
						Total int
						Items []json.RawMessage
					}
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if !tc.valid {
					if w.Code != http.StatusOK || body.Code != "VALIDATION_FAILED" {
						t.Errorf("%s search %q must reject NUL: status=%d body=%s", resource.name, tc.search, w.Code, w.Body)
					}
					continue
				}
				rows := 0
				if tc.matches {
					rows = resource.rows
				}
				if w.Code != http.StatusOK || body.Code != "OK" || body.Data.Total != rows || len(body.Data.Items) != rows {
					t.Errorf("%s valid search %q changed: want=%d status=%d body=%s", resource.name, tc.search, rows, w.Code, w.Body)
				}
			}
		})
	}
}
