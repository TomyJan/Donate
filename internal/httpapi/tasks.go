package httpapi

import (
	"net/http"
	"strconv"

	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
)

type TaskRetryInput struct {
	IDs []string `json:"ids"`
	Key string   `json:"idempotency_key"`
}

func taskPagination(r *http.Request) (int, int, error) {
	q := r.URL.Query()
	page, size := 1, 20
	for _, p := range []struct {
		key   string
		dest  *int
		limit int
	}{{"page", &page, 1000000}, {"page_size", &size, 100}} {
		if q.Has(p.key) {
			v, err := strconv.Atoi(q.Get(p.key))
			if err != nil || v < 1 || v > p.limit {
				return 0, 0, auth.ErrInvalid
			}
			*p.dest = v
		}
	}
	return page, size, nil
}
func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	page, size, err := taskPagination(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	q := r.URL.Query()
	v, err := s.Admin.Tasks(r.Context(), admin.TaskFilter{Page: page, Size: size, Search: q.Get("search"), Status: q.Get("status"), Kind: q.Get("kind"), Source: q.Get("source"), From: q.Get("from"), To: q.Get("to"), Sort: q.Get("sort"), Direction: q.Get("direction")})
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) task(w http.ResponseWriter, r *http.Request) {
	v, err := s.Admin.Task(r.Context(), r.URL.Query().Get("id"))
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) taskHistory(w http.ResponseWriter, r *http.Request) {
	page, size, err := taskPagination(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	v, err := s.Admin.TaskHistory(r.Context(), r.URL.Query().Get("id"), page, size)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) taskOverview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("range") != "custom" && (q.Has("from") || q.Has("to")) {
		s.failure(w, r, auth.ErrInvalid)
		return
	}
	v, err := s.Admin.TaskOverviewRange(r.Context(), q.Get("range"), q.Get("from"), q.Get("to"))
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) taskRetry(w http.ResponseWriter, r *http.Request) {
	var in TaskRetryInput
	if err := decode(r, &in); err != nil {
		s.failure(w, r, err)
		return
	}
	st := state(r)
	v, err := s.Admin.RetryTasks(r.Context(), st.Session.UserID, in.IDs, in.Key, st.ID)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.ok(w, r, v)
}
