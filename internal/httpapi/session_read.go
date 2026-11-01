package httpapi

import "net/http"

// Discovery must work after another tab replaces the browser's session. Every
// other API GET may bind its expected session before the handler reads data.
func (s *Server) readScopeOK(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return true
	}
	switch r.URL.Path {
	case "/api/v1/site/info", "/api/v1/user/profile", "/api/v1/user/login/status":
		return true
	}
	expected := r.Header.Get("X-Session-Scope")
	if expected == "" {
		return true
	} // Older clients retain the existing API.
	st := state(r)
	valid := expected == "guest" && st.Session == nil
	if st.Session != nil {
		valid = s.Auth.CheckCSRF(st.Token, expected)
	}
	if !valid {
		w.Header().Set("X-Session-Mismatch", "1")
		s.respond(w, r, http.StatusForbidden, "CSRF_FAILED", "会话已切换，请刷新账户资料", nil)
	}
	return valid
}
