package admin

import (
	"context"
	"errors"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/jobs"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"testing"
)

func TestRefreshLocationBatchConfirmationAndDisabled(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			s, actor := readFixture(t)
			s.Config.GeoMode = "local"
			if disabled {
				s.Config.GeoMode = "disabled"
			}
			ctx := context.Background()
			o := addReadOrder(t, s, actor, "refresh", "pending", "2026-10-30T00:00:00Z", "", "alipay", "", 1, false)
			oid, _ := storage.ParseID(o.ID)
			if _, err := s.Store.DB.Exec("UPDATE orders SET protocol='legacy',user_id=NULL,request_ip='1.1.1.1' WHERE id=?", oid); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.DB.Exec("DELETE FROM jobs WHERE order_id=?", oid); err != nil {
				t.Fatal(err)
			}
			missing := storage.NewID().String()
			op, err := s.Preview(ctx, actor, "refresh_location", []string{o.ID, missing, o.ID})
			if err != nil {
				t.Fatal("refresh preview", err)
			}
			if len(op.Items) != 2 {
				t.Fatal(op)
			}
			for _, item := range op.Items {
				want := "ready"
				if item.ID == missing {
					want = "not_found"
				} else if disabled {
					want = "disabled"
				}
				if item.Result != want {
					t.Fatalf("preview %+v want %s", item, want)
				}
			}
			result, err := s.Execute(ctx, actor, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			again, err := s.Execute(ctx, actor, op.ID)
			if err != nil || !again.Confirmed {
				t.Fatal(again, err)
			}
			for _, item := range result.Items {
				want := "queued"
				if item.ID == missing {
					want = "not_found"
				} else if disabled {
					want = "disabled"
				}
				if item.Result != want {
					t.Fatalf("execute %+v want %s", item, want)
				}
				if want == "queued" {
					j, err := jobs.NewQueue(s.Store, s.Log).Get(ctx, item.JobID)
					if err != nil || j.Kind != "geo" || j.Source != "admin_refresh" {
						t.Fatal(j, err)
					}
				}
			}
			var count int
			if err = s.Store.DB.QueryRow("SELECT count(*) FROM jobs WHERE kind='geo'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if disabled {
				want = 0
			}
			if count != want {
				t.Fatalf("jobs=%d want %d", count, want)
			}
			if err = s.Store.DB.QueryRow("SELECT count(*) FROM audit_events WHERE action='order.refresh_location'").Scan(&count); err != nil || count != 1 {
				t.Fatal("confirmation audit not idempotent", count, err)
			}
			got, err := s.Orders.Get(ctx, o.ID)
			if err != nil || got.Status != "pending" {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRefreshLocationRechecksAuthorizationAndDeletedTargets(t *testing.T) {
	s, actor := readFixture(t)
	s.Config.GeoMode = "online"
	ctx := context.Background()
	o := addReadOrder(t, s, actor, "refresh-delete", "pending", "2026-10-30T00:00:00Z", "", "alipay", "", 1, false)
	op, err := s.Preview(ctx, actor, "refresh_location", []string{o.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[actor] = false
	if _, err = s.Preview(ctx, actor, "refresh_location", []string{o.ID}); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, actor, op.ID); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal(err)
	}
	s.Config.AdminIDs[actor] = true
	if err = s.Orders.Delete(ctx, o.ID, actor); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(ctx, actor, op.ID)
	if err != nil || result.Items[0].Result != "not_found" {
		t.Fatal(result, err)
	}
	if _, err = s.Orders.Get(ctx, o.ID); !errors.Is(err, order.ErrNotFound) {
		t.Fatal(err)
	}
	var n int
	s.Store.DB.QueryRow("SELECT count(*) FROM jobs").Scan(&n)
	if n != 0 {
		t.Fatal(n)
	}
}
