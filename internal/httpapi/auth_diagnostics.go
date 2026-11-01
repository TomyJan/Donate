package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/go-chi/chi/v5"
)

func completionLogLevel(method, route, code string, status int, elapsed time.Duration) slog.Level {
	if status >= 400 || code != "OK" || elapsed >= time.Second {
		return slog.LevelInfo
	}
	if method == http.MethodHead {
		return slog.LevelDebug
	}
	if method == http.MethodGet {
		switch route {
		case "/health/live", "/health/ready", "/api/v1/site/info", "/api/v1/user/profile", "/api/v1/admin/task/list", "/api/v1/admin/task/overview", "/*":
			return slog.LevelDebug
		}
	}
	return slog.LevelInfo
}

func (s *Server) logAuthBoundary(r *http.Request, err error) {
	if auth.DiagnosticLogged(err) {
		return
	}
	fields := auth.DiagnosticFields(err)
	if len(fields) == 0 {
		if !errors.Is(err, auth.ErrUnauthorized) {
			return
		}
		fields = []any{"stage", "session_resolve", "reason", "session_missing"}
	}
	stage, reason := "", ""
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "stage" {
			stage, _ = fields[i+1].(string)
		}
		if fields[i] == "reason" {
			reason, _ = fields[i+1].(string)
		}
	}
	level := slog.LevelWarn
	if errors.Is(err, auth.ErrUnauthorized) && (stage == "session_resolve" || chi.RouteContext(r.Context()).RoutePattern() == "/api/v1/user/login/status") {
		level = slog.LevelDebug
	}
	st := state(r)
	fields = append(fields, "request_id", st.ID, "route", chi.RouteContext(r.Context()).RoutePattern())
	if st.Session != nil {
		fields = append(fields, "user_id", st.Session.UserID, "identity_id", st.Session.IdentityID)
	}
	s.Log.Log(r.Context(), level, fmt.Sprintf("请求 %s 的认证阶段 %s 未完成，原因 %s", st.ID, stage, reason), fields...)
}
