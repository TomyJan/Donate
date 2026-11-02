package httpapi

import "net/http"

func (s *Server) migrateOrder(w http.ResponseWriter, r *http.Request) {
	var input MigrateOrderInput
	if err := decode(r, &input); err != nil {
		s.failure(w, r, err)
		return
	}
	result, err := s.Admin.MigrateLegacyOrder(r.Context(), state(r).Session.UserID, input.ID, input.UserID)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.ok(w, r, result)
}
