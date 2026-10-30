package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/jobs"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/payment"
	"github.com/TomyJan/Donate/internal/storage"
	"github.com/go-chi/chi/v5"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Tag     string `json:"tag"`
	BuiltAt string `json:"built_at"`
}
type Server struct {
	Config   config.Config
	Auth     *auth.Service
	Orders   *order.Service
	Payments *payment.Service
	Admin    *admin.Service
	Queue    *jobs.Queue
	Log      *slog.Logger
	Assets   fs.FS
	Version  Version
	Degraded *atomic.Bool
	limits   limiter
}
type requestState struct {
	ID             string
	Session        *auth.Session
	Token, Code    string
	AuthError      error
	Status         int
	SessionRevoked bool
}
type stateKey struct{}

func state(r *http.Request) *requestState { return r.Context().Value(stateKey{}).(*requestState) }

type Envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

func (s *Server) sessionName() string {
	if s.Config.Development {
		return "donate_session_dev"
	}
	return "__Host-donate_session"
}
func (s *Server) cookieName(part string) string {
	if s.Config.Development {
		return "donate_" + part + "_dev"
	}
	return "__Host-donate_" + part
}
func cookie(r *http.Request, name string) string {
	c, e := r.Cookie(name)
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *Server) setCookie(w http.ResponseWriter, name, value string, expiry time.Time) {
	age := int(time.Until(expiry).Seconds())
	if value == "" {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: !s.Config.Development, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: age})
}
func (s *Server) anonymousCSRF(token string) string {
	h := hmac.New(sha256.New, s.Config.Secret)
	h.Write([]byte("anonymous-csrf:" + token))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Server) respond(w http.ResponseWriter, r *http.Request, status int, code, message string, data any) {
	st := state(r)
	st.Code = code
	st.Status = status
	if code == "OK" && r.Method == "POST" && st.Session != nil && !st.SessionRevoked && !strings.HasSuffix(r.URL.Path, "/logout") {
		session, e := s.Auth.Renew(r.Context(), st.Token)
		if e != nil {
			s.failure(w, r, e)
			return
		}
		s.setCookie(w, s.sessionName(), st.Token, session.ExpiresAt)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{code, message, data, st.ID})
}
func (s *Server) ok(w http.ResponseWriter, r *http.Request, v any) { s.respond(w, r, 200, "OK", "", v) }
func (s *Server) failure(w http.ResponseWriter, r *http.Request, e error) {
	status, code, message := 200, "INTERNAL_ERROR", "请求未完成"
	switch {
	case errors.Is(e, auth.ErrUnauthorized):
		status, code, message = 401, "UNAUTHORIZED", "请登录"
	case errors.Is(e, auth.ErrBanned):
		status, code, message = 403, "ACCOUNT_BANNED", "账号已封禁"
	case errors.Is(e, auth.ErrConflict):
		code, message = "IDENTITY_CONFLICT", e.Error()
	case errors.Is(e, auth.ErrInvalid), errors.Is(e, order.ErrInvalid):
		code, message = "VALIDATION_FAILED", "请检查输入"
	case errors.Is(e, order.ErrNotFound):
		code, message = "NOT_FOUND", "记录不存在"
	case errors.Is(e, order.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "无权访问"
	case errors.Is(e, order.ErrConflict):
		code, message = "IDEMPOTENCY_CONFLICT", e.Error()
	case errors.Is(e, order.ErrBusy):
		code, message = "RATE_LIMITED", "请稍后重试"
	case errors.Is(e, order.ErrNotPayable):
		code, message = "ORDER_NOT_PAYABLE", e.Error()
	case errors.Is(e, order.ErrMismatch):
		code, message = "PAYMENT_MISMATCH", "支付核验未通过"
	case errors.Is(e, auth.ErrUpstream), errors.Is(e, payment.ErrUnavailable), errors.Is(e, payment.ErrProtocol):
		code, message = "UPSTREAM_UNAVAILABLE", "第三方服务暂不可用"
	case errors.Is(e, admin.ErrSelfBan):
		code, message = "SELF_BAN_FORBIDDEN", e.Error()
	case errors.Is(e, admin.ErrLastIdentity):
		code, message = "LAST_IDENTITY_FORBIDDEN", e.Error()
	default:
		status = 500
		s.Log.Error(fmt.Sprintf("请求 %s 无法完成，路由 %s", state(r).ID, chi.RouteContext(r.Context()).RoutePattern()), "request_id", state(r).ID, "error", e)
	}
	s.respond(w, r, status, code, message, nil)
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return auth.ErrInvalid
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return auth.ErrInvalid
	}
	return nil
}
func (s *Server) need(w http.ResponseWriter, r *http.Request, adminOnly bool) bool {
	st := state(r)
	if st.AuthError != nil {
		s.failure(w, r, st.AuthError)
		return false
	}
	if st.Session == nil {
		s.failure(w, r, auth.ErrUnauthorized)
		return false
	}
	if adminOnly && !st.Session.Admin {
		s.respond(w, r, 403, "FORBIDDEN", "无权访问", nil)
		return false
	}
	return true
}
func (s *Server) csrfOK(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != s.Config.PublicURL || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		s.respond(w, r, 403, "CSRF_FAILED", "请求来源无效", nil)
		return false
	}
	st := state(r)
	provided := r.Header.Get("X-CSRF-Token")
	valid := false
	if st.Session != nil {
		valid = s.Auth.CheckCSRF(st.Token, provided)
	} else {
		token := cookie(r, s.cookieName("csrf"))
		valid = len(token) == 43 && hmac.Equal([]byte(s.anonymousCSRF(token)), []byte(provided))
	}
	if !valid {
		s.respond(w, r, 403, "CSRF_FAILED", "请求凭证无效", nil)
	}
	return valid
}
func (s *Server) route(fn http.HandlerFunc, level int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if level > 0 && !s.need(w, r, level == 2) {
			return
		}
		if state(r).AuthError != nil && errors.Is(state(r).AuthError, auth.ErrBanned) {
			s.failure(w, r, auth.ErrBanned)
			return
		}
		if r.Method == "POST" && !s.csrfOK(w, r) {
			return
		}
		fn(w, r)
	}
}
func (s *Server) Handler() http.Handler {
	mux := chi.NewRouter()
	mux.Use(s.middleware)
	mux.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { s.ok(w, r, map[string]bool{"alive": true}) })
	mux.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if (s.Degraded != nil && s.Degraded.Load()) || s.Auth.Store.DB.PingContext(ctx) != nil {
			s.respond(w, r, 503, "NOT_READY", "服务未就绪", nil)
			return
		}
		s.ok(w, r, map[string]bool{"ready": true})
	})
	mux.Route("/api/v1", func(m chi.Router) {
		m.Get("/site/info", s.route(s.siteInfo, 0))
		m.Post("/user/login", s.route(s.login, 0))
		m.Get("/user/oauth/callback/{provider}", s.callback)
		m.Get("/user/login/status", s.route(s.flowStatus, 0))
		m.Post("/user/login/qq/complete", s.route(s.completeQQ, 0))
		m.Get("/user/profile", s.route(s.profile, 1))
		m.Post("/user/session/renew", s.route(func(w http.ResponseWriter, r *http.Request) { s.ok(w, r, nil) }, 1))
		m.Post("/user/logout", s.route(s.logout, 1))
		m.Post("/user/identity/bind", s.route(s.bind, 1))
		m.Get("/order/list", s.route(s.list, 0))
		m.Get("/order/detail", s.route(s.detail, 1))
		m.Post("/order/create", s.route(s.create, 1))
		m.Post("/order/payment/start", s.route(s.startPayment, 1))
		m.Get("/payment/notify", s.notify)
		m.Post("/payment/notify", s.notify)
		m.Get("/payment/return", s.paymentReturn)
		m.Get("/admin/order/list", s.route(s.adminOrders, 2))
		m.Get("/admin/order/detail", s.route(s.detail, 2))
		m.Post("/admin/order/action/preview", s.route(s.preview, 2))
		m.Post("/admin/order/action/execute", s.route(s.execute, 2))
		m.Post("/admin/order/reconcile", s.route(s.reconcile, 2))
		m.Get("/admin/operation/detail", s.route(s.operation, 2))
		m.Post("/admin/order/export", s.route(s.export, 2))
		m.Get("/admin/user/list", s.route(s.users, 2))
		m.Get("/admin/user/detail", s.route(s.user, 2))
		m.Post("/admin/user/ban", s.route(s.ban, 2))
		m.Post("/admin/user/unban", s.route(s.ban, 2))
		m.Post("/admin/user/identity/unbind", s.route(s.unbindIdentity, 2))
		m.Get("/admin/overview", s.route(s.overview, 2))
		m.Get("/admin/audit/list", s.route(s.audit, 2))
	})
	mux.Get("/*", s.static)
	return mux
}
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &requestState{ID: storage.NewID().String(), Code: "OK", Status: 200}
		r = r.WithContext(context.WithValue(r.Context(), stateKey{}, st))
		started := time.Now()
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error(fmt.Sprintf("请求 %s 发生未处理错误", st.ID), "request_id", st.ID)
				s.respond(w, r, 500, "INTERNAL_ERROR", "请求未完成", nil)
			}
			uid := ""
			if st.Session != nil {
				uid = st.Session.UserID
			}
			pattern := chi.RouteContext(r.Context()).RoutePattern()
			s.Log.Info(fmt.Sprintf("请求 %s %s %s 完成，用户 %s，HTTP %d，业务 %s，耗时 %d 毫秒", st.ID, r.Method, pattern, uid, st.Status, st.Code, time.Since(started).Milliseconds()), "request_id", st.ID, "route", pattern, "user_id", uid, "status", st.Status, "code", st.Code, "duration_ms", time.Since(started).Milliseconds())
		}()
		w.Header().Set("X-Request-ID", st.ID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://cdn.amoe.cc; font-src 'self' https://cdn.amoe.cc https://font.sec.miui.com https://cdn-file.hyperos.mi.com https://registry.npmmirror.com/@fontsource/source-code-pro/ data:; img-src 'self' https: data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self' "+s.Config.EpayURL)
		if !s.Config.Development {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		st.Token = cookie(r, s.sessionName())
		if st.Token != "" {
			session, e := s.Auth.Resolve(r.Context(), st.Token)
			if e == nil {
				st.Session = &session
			} else {
				st.AuthError = e
			}
		}
		if !s.limits.allow(s.clientIP(r)+":http", 600, time.Minute) {
			s.respond(w, r, 429, "RATE_LIMITED", "请稍后重试", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) clientIP(r *http.Request) string {
	a, e := netip.ParseAddrPort(r.RemoteAddr)
	var ip netip.Addr
	if e == nil {
		ip = a.Addr().Unmap()
	} else {
		ip, _ = netip.ParseAddr(r.RemoteAddr)
		ip = ip.Unmap()
	}
	trusted := func(a netip.Addr) bool {
		for _, p := range s.Config.TrustedProxies {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	if trusted(ip) {
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if len(chain) <= 32 {
			for i := len(chain) - 1; i >= 0; i-- {
				if !trusted(ip) {
					break
				}
				next, e := netip.ParseAddr(strings.TrimSpace(chain[i]))
				if e != nil {
					break
				}
				ip = next.Unmap()
			}
		}
	}
	if !ip.IsValid() {
		return "0.0.0.0"
	}
	return ip.String()
}

type limitEntry struct {
	count int
	until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]limitEntry
}

func (l *limiter) allow(key string, n int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]limitEntry{}
	}
	now := time.Now()
	v := l.entries[key]
	if now.After(v.until) {
		if len(l.entries) >= 8192 {
			for k, e := range l.entries {
				if now.After(e.until) {
					delete(l.entries, k)
				}
			}
			if len(l.entries) >= 8192 {
				return false
			}
		}
		v = limitEntry{until: now.Add(window)}
	}
	if v.count >= n {
		return false
	}
	v.count++
	l.entries[key] = v
	return true
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "." || p == "admin" || p == "auth/complete" {
		p = "index.html"
	} else if !strings.HasPrefix(p, "assets/") {
		state(r).Status = 404
		http.NotFound(w, r)
		return
	}
	if s.Assets == nil {
		http.Error(w, "请先构建前端", 503)
		return
	}
	b, e := fs.ReadFile(s.Assets, p)
	if e != nil {
		state(r).Status = 404
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(p, ".js") {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else if strings.HasSuffix(p, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else if strings.HasSuffix(p, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public,max-age=31536000,immutable")
	}
	_, _ = w.Write(b)
}
func filter(r *http.Request) (admin.Filter, error) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	if size == 0 {
		size = 20
	}
	f := admin.Filter{UserID: q.Get("user_id"), Search: q.Get("search"), Status: q.Get("status"), Channel: q.Get("channel"), Visibility: q.Get("visibility"), From: q.Get("from"), To: q.Get("to"), Sort: q.Get("sort"), Direction: q.Get("direction"), Page: page, Size: size, Action: q.Get("action")}
	for _, p := range []struct {
		key  string
		dest **int64
	}{{"minimum", &f.Min}, {"maximum", &f.Max}} {
		if q.Get(p.key) != "" {
			v, e := strconv.ParseInt(q.Get(p.key), 10, 64)
			if e != nil || v < 0 {
				return f, auth.ErrInvalid
			}
			*p.dest = &v
		}
	}
	return f, nil
}
