package admin

import (
	"context"
	"testing"

	"github.com/TomyJan/Donate/internal/storage"
)

func TestAuditTargetSortUsesTaskResourceAcrossPages(t *testing.T) {
	s, _, actor, now := taskFixture(t)
	actorID, _ := storage.ParseID(actor)
	// Fixed audit IDs deliberately order the resources in reverse.
	for i, resource := range []string{"xdb6", "xdb4", "mmdb"} {
		id, _ := storage.ParseID(taskGeoIP(t, s, resource, "failed"))
		auditID := make([]byte, 16)
		auditID[15] = byte(i + 1)
		if _, err := s.Store.DB.Exec("INSERT INTO audit_events(id,actor_id,action,target_type,target_id,result,created_at) VALUES(?,?,'task.retry','task',?,'queued',?)", auditID, actorID, id, now.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		direction string
		want      []string
	}{
		{"asc", []string{"mmdb", "xdb4", "xdb6"}},
		{"desc", []string{"xdb6", "xdb4", "mmdb"}},
	} {
		for i, want := range tc.want {
			page, err := s.Audit(context.Background(), Filter{Sort: "target", Direction: tc.direction, Page: i + 1, Size: 1})
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 3 || len(page.Items) != 1 || page.Items[0].Target != want {
				t.Fatalf("%s page %d: %+v, want %s", tc.direction, i+1, page, want)
			}
		}
	}
}

func TestAuditTargetSortKeepsMixedAndDeletedTargets(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	_, orderID := taskOrder(t, s, q, "geo", "automatic", "alpha")
	resourceID := taskGeoIP(t, s, "mmdb", "failed")
	deletedID := taskGeoIP(t, s, "xdb4", "failed")
	actorBytes, _ := storage.ParseID(actor)
	for i, target := range []struct{ kind, id string }{
		{"task", resourceID}, {"user", actor}, {"order", orderID}, {"task", deletedID},
	} {
		id, _ := storage.ParseID(target.id)
		auditID := make([]byte, 16)
		auditID[15] = byte(i + 1)
		if _, err := s.Store.DB.Exec("INSERT INTO audit_events(id,actor_id,action,target_type,target_id,result,created_at) VALUES(?,?,'test',?,?,'ok',?)", auditID, actorBytes, target.kind, id, now.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	deletedBytes, _ := storage.ParseID(deletedID)
	if _, err := s.Store.DB.Exec("DELETE FROM jobs WHERE id=?", deletedBytes); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		direction string
		want      []string
	}{
		{"asc", []string{deletedID, orderID, resourceID, actor}},
		{"desc", []string{actor, resourceID, orderID, deletedID}},
	} {
		for pageNumber := 1; pageNumber <= 2; pageNumber++ {
			page, err := s.Audit(context.Background(), Filter{Sort: "target", Direction: tc.direction, Page: pageNumber, Size: 2})
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 4 || len(page.Items) != 2 {
				t.Fatalf("unexpected page: %+v", page)
			}
			for i, item := range page.Items {
				if item.TargetID != tc.want[(pageNumber-1)*2+i] {
					t.Fatalf("%s page %d item %d: %+v", tc.direction, pageNumber, i, item)
				}
				if item.TargetID == deletedID && (item.TargetExists || item.Target != "") {
					t.Fatalf("deleted task shown as existing: %+v", item)
				}
			}
		}
	}
}
