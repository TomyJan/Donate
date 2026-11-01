package admin

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestOrderAndTaskSearchPreservesLiteralNicknameCase(t *testing.T) {
	for _, tc := range []struct {
		name, target, search string
		others               []string
	}{
		{"unicode-upper", "Änne", "Änne", []string{"änne", "ÄnXne"}},
		{"unicode-lower", "änne", "änne", []string{"Änne", "änXne"}},
		{"unicode-substring", "PrefixÄnneSuffix", "Änne", []string{"PrefixänneSuffix", "PrefixÄnXneSuffix"}},
		{"unicode-ascii-case", "ÄNNE", "ÄnNe", []string{"änne", "ÄnXne"}},
		{"unicode-patterns", "Élodie_%", "Élodie_%", []string{"ÉlodieXextra", "élodie_%"}},
		{"unicode-combined-literals", `Élodie\_%Suffix`, `Élodie\_%`, []string{`Élodie\XXSuffix`, `élodie\_%Suffix`}},
		{"unicode-trailing-backslash", `Élodie\`, `Élodie\`, []string{"Élodie", `élodie\`}},
		{"single-underscore", "Élodie_Name", "_", []string{"ÉlodieXName", "élodieName"}},
		{"single-percent", "Élodie%Name", "%", []string{"ÉlodieExtraName", "élodieName"}},
		{"single-backslash", `Élodie\Name`, `\`, []string{"ÉlodieName", "élodieName"}},
		{"combined-pattern-substring", `Élodie\_%Suffix`, `\_%`, []string{`Élodie\XXSuffix`, "élodieSuffix"}},
		{"underscore", "Nick_Name", "Nick_Name", []string{"NickXName", "Unrelated"}},
		{"percent", "Nick%Name", "Nick%Name", []string{"NickExtraName", "Unrelated"}},
		{"backslash", `Nick\Name`, `Nick\Name`, []string{"NickName", "Unrelated"}},
		{"ascii-case", "PrefixAliceSuffix", "aLiCe", []string{"PrefixAliciaSuffix", "Unrelated"}},
		{"chinese-substring", "中文昵称", "文昵称", []string{"中文X昵称", "无关"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fallback := range []bool{false, true} {
				t.Run(fmt.Sprintf("identity-fallback=%t", fallback), func(t *testing.T) {
					s, _, _, _ := taskFixture(t)
					ctx := context.Background()
					login := auth.New(s.Store, config.Config{}, s.Log)
					var target order.Order
					for i, nickname := range append([]string{tc.target}, tc.others...) {
						identityName := nickname
						if !fallback {
							identityName = fmt.Sprintf("Current identity %d", i)
						}
						owner, err := login.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "literal-search", Subject: fmt.Sprint(i), Nickname: identityName})
						if err != nil {
							t.Fatal(err)
						}
						created, err := s.Orders.Create(ctx, order.CreateInput{UserID: owner.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "literal-search"})
						if err != nil {
							t.Fatal(err)
						}
						id, err := storage.ParseID(created.ID)
						if err != nil {
							t.Fatal(err)
						}
						if !fallback {
							if _, err = s.Store.DB.ExecContext(ctx, "UPDATE orders SET donor_nickname=? WHERE id=?", nickname, id); err != nil {
								t.Fatal(err)
							}
						}
						var snapshot, identity string
						if err = s.Store.DB.QueryRowContext(ctx, "SELECT o.donor_nickname,i.nickname FROM orders o JOIN users u ON u.id=o.user_id JOIN identities i ON i.id=u.public_identity_id WHERE o.id=?", id).Scan(&snapshot, &identity); err != nil {
							t.Fatal(err)
						}
						if identity != identityName {
							t.Fatalf("current identity premise: got=%q want=%q", identity, identityName)
						}
						if fallback {
							if snapshot != "" || identity != nickname {
								t.Fatalf("identity fallback premise: snapshot=%q identity=%q want=%q", snapshot, identity, nickname)
							}
						} else if snapshot != nickname || identity == snapshot {
							t.Fatalf("frozen snapshot premise: snapshot=%q identity=%q want snapshot=%q and a different identity", snapshot, identity, nickname)
						}
						if i == 0 {
							target = created
						}
					}
					assertOrderAndTaskSearch(t, s, tc.search, target.ID)
					if !fallback {
						orders, err := s.OrdersList(ctx, Filter{Search: "Current identity 0", Size: 100})
						if err != nil || orders.Total != 0 || len(orders.Items) != 0 {
							t.Errorf("snapshot order search fell back to current identity: %+v err=%v", orders, err)
						}
						tasks, err := s.Tasks(ctx, TaskFilter{Search: "Current identity 0", Size: 100})
						if err != nil || tasks.Total != 0 || len(tasks.Items) != 0 {
							t.Errorf("snapshot task search fell back to current identity: %+v err=%v", tasks, err)
						}
					}
				})
			}
		})
	}
}

func assertOrderAndTaskSearch(t *testing.T, s *Service, search, orderID string) {
	t.Helper()
	ctx := context.Background()
	orders, err := s.OrdersList(ctx, Filter{Search: search, Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if orders.Total != 1 || len(orders.Items) != 1 || orders.Items[0].ID != orderID {
		t.Errorf("order search %q: total=%d items=%+v; want only %s", search, orders.Total, orders.Items, orderID)
	}
	tasks, err := s.Tasks(ctx, TaskFilter{Search: search, Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if tasks.Total != 2 || len(tasks.Items) != 2 {
		t.Errorf("task search %q: total=%d items=%+v; want two tasks for %s", search, tasks.Total, tasks.Items, orderID)
	}
	for _, task := range tasks.Items {
		if task.OrderID != orderID {
			t.Errorf("task search %q matched another order: %+v", search, task)
		}
	}
}

func TestOrderAndTaskNicknameSearchPreservesIdentifierRules(t *testing.T) {
	s, _, _, _ := taskFixture(t)
	ctx := context.Background()
	login := auth.New(s.Store, config.Config{}, s.Log)
	var target order.Order
	var userID string
	for i, nickname := range []string{"Target", "Other"} {
		owner, err := login.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "identifier-search", Subject: fmt.Sprint(i), Nickname: nickname})
		if err != nil {
			t.Fatal(err)
		}
		created, err := s.Orders.Create(ctx, order.CreateInput{UserID: owner.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "identifier-search"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			target, userID = created, owner.Session.UserID
		}
	}
	for _, id := range []string{target.ID, target.MerchantOrderNo} {
		for _, search := range []string{id, strings.ToUpper(id), id[19:], strings.ReplaceAll(id, "-", "")[16:]} {
			t.Run(search, func(t *testing.T) { assertOrderAndTaskSearch(t, s, search, target.ID) })
		}
	}
	for _, search := range []string{userID, strings.ToUpper(userID), userID[19:], strings.ReplaceAll(userID, "-", "")[16:]} {
		page, err := s.OrdersList(ctx, Filter{Search: search, Size: 100})
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != target.ID {
			t.Errorf("user UUID search %q: %+v err=%v", search, page, err)
		}
	}
	page, err := s.Tasks(ctx, TaskFilter{Search: target.ID, Size: 100})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("target tasks: %+v err=%v", page, err)
	}
	want := page.Items[0]
	for _, search := range []string{want.ID, strings.ToUpper(want.ID), want.ID[19:], strings.ReplaceAll(want.ID, "-", "")[16:]} {
		page, err := s.Tasks(ctx, TaskFilter{Search: search, Size: 100})
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != want.ID {
			t.Errorf("task UUID search %q: %+v err=%v", search, page, err)
		}
	}
}
