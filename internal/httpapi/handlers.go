package httpapi

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/payment"
	"github.com/TomyJan/Donate/internal/storage"
	"github.com/go-chi/chi/v5"
	"net/http"
	"time"
)

func (s *Server) siteInfo(w http.ResponseWriter, r *http.Request) {
	token := cookie(r, s.cookieName("csrf"))
	if len(token) != 43 {
		token = auth.RandomToken()
		s.setCookie(w, s.cookieName("csrf"), token, time.Now().Add(30*24*time.Hour))
	}
	providers := []string{}
	details := s.Config.LoginProviders()
	for _, provider := range details {
		providers = append(providers, provider.ID)
	}
	csrf := s.anonymousCSRF(token)
	if state(r).Session != nil {
		csrf = state(r).Session.CSRF
	}
	s.ok(w, r, map[string]any{"name": "捐赠 TomyJan", "logo": "https://public-cdn.tomys.top/head.png", "version": s.Version, "providers": providers, "provider_details": details, "payment_enabled": s.Payments.Gateway.Ready(), "csrf_token": csrf})
}

func (s *Server) begin(w http.ResponseWriter, r *http.Request, intent string) {
	if !s.limits.allow(s.clientIP(r)+":login", 15, time.Minute) {
		s.failure(w, r, order.ErrBusy)
		return
	}
	var in LoginInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Auth.BeginFlow(r.Context(), in.Provider, intent, state(r).Session, in.ReturnTo)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.setCookie(w, s.cookieName("flow"), v.Cookie, time.Now().Add(10*time.Minute))
	s.ok(w, r, v)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) { s.begin(w, r, "login") }
func (s *Server) bind(w http.ResponseWriter, r *http.Request)  { s.begin(w, r, "bind") }
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	v, e := payment.ParseValues(r.URL.Query())
	if e != nil {
		s.respond(w, r, 200, "VALIDATION_FAILED", "授权回调参数无效", nil)
		return
	}
	result, e := s.Auth.Callback(r.Context(), chi.URLParam(r, "provider"), v["state"], v["code"], cookie(r, s.cookieName("flow")), state(r).Session)
	if e != nil {
		s.Log.Warn(fmt.Sprintf("授权回调请求 %s 未完成，需重新发起登录", state(r).ID), "request_id", state(r).ID)
		state(r).Status = 303
		http.Redirect(w, r, "/auth/complete?error=authorization", http.StatusSeeOther)
		return
	}
	s.finishFlow(w, r, result)
}
func (s *Server) finishFlow(w http.ResponseWriter, r *http.Request, result auth.FlowResult) {
	if result.Credentials != nil {
		s.setCookie(w, s.sessionName(), result.Credentials.Token, result.Credentials.Session.ExpiresAt)
	}
	if result.NeedQQ {
		http.Redirect(w, r, "/auth/complete", 303)
		state(r).Status = 303
		return
	}
	s.setCookie(w, s.cookieName("flow"), "", time.Unix(0, 0))
	state(r).Status = 303
	http.Redirect(w, r, result.ReturnTo, 303)
}
func (s *Server) flowStatus(w http.ResponseWriter, r *http.Request) {
	v, e := s.Auth.FlowStatus(r.Context(), cookie(r, s.cookieName("flow")))
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) completeQQ(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow(s.clientIP(r)+":qq", 10, time.Minute) {
		s.failure(w, r, order.ErrBusy)
		return
	}
	var in QQInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Auth.CompleteQQ(r.Context(), cookie(r, s.cookieName("flow")), in.Number, state(r).Session)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	if v.Credentials != nil {
		s.setCookie(w, s.sessionName(), v.Credentials.Token, v.Credentials.Session.ExpiresAt)
		// The login transaction revoked the old token. Renew the newly issued session.
		state(r).Token = v.Credentials.Token
		state(r).Session = &v.Credentials.Session
	}
	s.setCookie(w, s.cookieName("flow"), "", time.Unix(0, 0))
	s.ok(w, r, map[string]string{"return_to": v.ReturnTo})
}
func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	v, e := s.Auth.Profile(r.Context(), *state(r).Session)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if e := s.Auth.Logout(r.Context(), state(r).Token); e != nil {
		s.failure(w, r, e)
		return
	}
	s.setCookie(w, s.sessionName(), "", time.Unix(0, 0))
	s.ok(w, r, nil)
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	user := ""
	if state(r).Session != nil {
		user = state(r).Session.UserID
	}
	mine := r.URL.Query().Get("only_mine") == "true"
	if mine && user == "" {
		s.failure(w, r, auth.ErrUnauthorized)
		return
	}
	v, e := s.Orders.List(r.Context(), user, mine, r.URL.Query().Get("cursor"))
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	o, e := s.Orders.Get(r.Context(), id)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	session := state(r).Session
	if o.UserID != session.UserID && !session.Admin {
		s.failure(w, r, order.ErrForbidden)
		return
	}
	full := session.Admin && len(r.URL.Path) > 13 && r.URL.Path[:13] == "/api/v1/admin"
	v, e := s.Admin.Detail(r.Context(), id, full)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if !s.Payments.Gateway.Ready() {
		s.failure(w, r, payment.ErrUnavailable)
		return
	}
	var in CreateOrderInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	o, e := s.Orders.Create(r.Context(), order.CreateInput{UserID: state(r).Session.UserID, AmountFen: in.Amount, Channel: in.Channel, Message: in.Message, Key: in.Key, IP: s.clientIP(r)})
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, o)
}
func (s *Server) startPayment(w http.ResponseWriter, r *http.Request) {
	var in IDInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Payments.Start(r.Context(), in.ID, state(r).Session.UserID)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) notify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	values := r.URL.Query()
	if r.Method == "POST" {
		if e := r.ParseForm(); e != nil {
			state(r).Code = "INVALID_NOTIFY"
			_, _ = w.Write([]byte("fail"))
			return
		}
		if len(r.URL.Query()) > 0 {
			state(r).Code = "INVALID_NOTIFY"
			_, _ = w.Write([]byte("fail"))
			return
		}
		values = r.PostForm
	}
	v, e := payment.ParseValues(values)
	if e == nil {
		_, e = s.Payments.Notify(r.Context(), v, state(r).ID)
	}
	if e != nil {
		state(r).Code = "NOTIFY_REJECTED"
		if !errors.Is(e, order.ErrNotFound) && !errors.Is(e, payment.ErrProtocol) && !errors.Is(e, order.ErrMismatch) && !errors.Is(e, payment.ErrUnavailable) {
			state(r).Status = 500
			w.WriteHeader(500)
		}
		_, _ = w.Write([]byte("fail"))
		return
	}
	_, _ = w.Write([]byte("success"))
}
func (s *Server) paymentReturn(w http.ResponseWriter, r *http.Request) {
	target := "/?payment=returned"
	if s.limits.allow(s.clientIP(r)+":return", 10, time.Minute) {
		v, e := payment.ParseValues(r.URL.Query())
		if e == nil && len(v["out_trade_no"]) <= 128 {
			o, e := order.Scan(s.Orders.Store.DB.QueryRowContext(r.Context(), "SELECT "+order.Columns+" FROM orders WHERE merchant_order_no=?", v["out_trade_no"]))
			if e == nil {
				target += "&order=" + o.ID
				if _, err := s.Queue.Enqueue(r.Context(), "payment", o.ID, "browser_return", "", state(r).ID); err != nil {
					s.Log.WarnContext(r.Context(), "订单 "+o.ID+" 支付返回核验任务入队失败", "order_id", o.ID, "request_id", state(r).ID, "error", err)
				}
			}
		}
	}
	state(r).Status = 303
	http.Redirect(w, r, target, 303)
}
func (s *Server) adminOrders(w http.ResponseWriter, r *http.Request) {
	f, e := filter(r)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Admin.OrdersList(r.Context(), f)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	f, e := filter(r)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Admin.Users(r.Context(), f)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	v, e := s.Admin.User(r.Context(), r.URL.Query().Get("id"))
	if errors.Is(e, sql.ErrNoRows) {
		e = order.ErrNotFound
	}
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("range") != "custom" && (q.Has("from") || q.Has("to")) {
		s.failure(w, r, auth.ErrInvalid)
		return
	}
	v, e := s.Admin.OverviewRange(r.Context(), q.Get("range"), q.Get("from"), q.Get("to"))
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	f, e := filter(r)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Admin.Audit(r.Context(), f)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var in PreviewInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Admin.Preview(r.Context(), state(r).Session.UserID, in.Action, in.IDs)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) execute(w http.ResponseWriter, r *http.Request) {
	var in OperationInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	v, e := s.Admin.Execute(r.Context(), state(r).Session.UserID, in.ID)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) operation(w http.ResponseWriter, r *http.Request) {
	v, e := s.Admin.Operation(r.Context(), state(r).Session.UserID, r.URL.Query().Get("id"))
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, v)
}
func (s *Server) ban(w http.ResponseWriter, r *http.Request) {
	var in IDInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	ban := r.URL.Path == "/api/v1/admin/user/ban"
	if e := s.Admin.Ban(r.Context(), state(r).Session.UserID, in.ID, ban); e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, nil)
}
func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	var in ReconcileInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	if _, e := s.Orders.Get(r.Context(), in.ID); e != nil {
		s.failure(w, r, e)
		return
	}
	if !s.limits.allow(state(r).Session.UserID+":reconcile", 30, time.Minute) {
		s.failure(w, r, order.ErrBusy)
		return
	}
	id, e := s.Queue.EnqueueOnce(r.Context(), in.ID, state(r).Session.UserID, state(r).ID, in.Key)
	if e != nil {
		s.failure(w, r, e)
		return
	}
	s.ok(w, r, map[string]string{"job_id": id})
}
func (s *Server) unbindIdentity(w http.ResponseWriter, r *http.Request) {
	var in UnbindIdentityInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	st := state(r)
	if e := s.Admin.UnbindIdentity(r.Context(), st.Session.UserID, in.ID, in.IdentityID); e != nil {
		s.failure(w, r, e)
		return
	}
	uid, _ := storage.ParseID(in.ID)
	iid, _ := storage.ParseID(in.IdentityID)
	if storage.IDString(uid) == st.Session.UserID && storage.IDString(iid) == st.Session.IdentityID {
		// The mutation already removed this credential. Do not turn its success
		// into a renewal error or issue the revoked cookie again.
		st.SessionRevoked = true
		s.setCookie(w, s.sessionName(), "", time.Unix(0, 0))
	}
	s.ok(w, r, nil)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	var in OperationInput
	if e := decode(r, &in); e != nil {
		s.failure(w, r, e)
		return
	}
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	if e := s.Admin.Export(r.Context(), state(r).Session.UserID, in.ID, &b); e != nil {
		s.failure(w, r, e)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="donate-orders.csv"`)
	_, _ = w.Write(b.Bytes())
}
