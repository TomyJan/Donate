package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/TomyJan/Donate/internal/storage"
)

type FlowStart struct {
	URL           string `json:"authorization_url"`
	State         string `json:"-"`
	Cookie        string `json:"-"`
	BrowserFamily string `json:"-"`
}
type flowSecret struct {
	Verifier string
	ReturnTo string
	Identity flowIdentity
}
type flowIdentity struct{ Provider, Issuer, Subject, Nickname, AvatarURL, QQNumber, QQOpenID, QQUnionID string }

func (p flowIdentity) profile() Identity {
	return Identity{Provider: p.Provider, Issuer: p.Issuer, Subject: p.Subject, Nickname: p.Nickname, AvatarURL: p.AvatarURL, QQNumber: p.QQNumber, QQOpenID: p.QQOpenID, QQUnionID: p.QQUnionID}
}
func secretIdentity(p Identity) flowIdentity {
	return flowIdentity{Provider: p.Provider, Issuer: p.Issuer, Subject: p.Subject, Nickname: p.Nickname, AvatarURL: p.AvatarURL, QQNumber: p.QQNumber, QQOpenID: p.QQOpenID, QQUnionID: p.QQUnionID}
}

type flow struct {
	ID                                        []byte
	Provider, Intent, Nonce, SessionID, Stage string
	Expires                                   int64
	Attempts                                  int
	Secret                                    flowSecret
}
type FlowStatus struct {
	Stage    string `json:"stage"`
	Provider string `json:"provider"`
	ReturnTo string `json:"return_to"`
}
type FlowResult struct {
	Credentials *Credentials
	ReturnTo    string
	NeedQQ      bool
}

func (s *Service) seal(v flowSecret) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(s.Config.Secret)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return a.Seal(nonce, nonce, b, nil), nil
}
func (s *Service) unseal(b []byte) (flowSecret, error) {
	var v flowSecret
	block, e := aes.NewCipher(s.Config.Secret)
	if e != nil {
		return v, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil || len(b) < a.NonceSize() {
		return v, ErrUnauthorized
	}
	plain, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], nil)
	if e != nil {
		return v, ErrUnauthorized
	}
	e = json.Unmarshal(plain, &v)
	return v, e
}
func validReturn(p string) string {
	if p == "/" || p == "/admin" {
		return p
	}
	u, err := url.Parse(p)
	if err == nil && u.Scheme == "" && u.Host == "" && u.Path == "/" {
		values, err := url.ParseQuery(u.RawQuery)
		if err == nil && len(values["payment"]) == 1 && values.Get("payment") == "returned" && len(values["order"]) == 1 {
			if id, err := storage.ParseID(values.Get("order")); err == nil {
				return "/?payment=returned&order=" + storage.IDString(id)
			}
		}
	}
	return "/"
}
func (s *Service) provider(ctx context.Context) (*oidc.Provider, *upstreamTrace, error) {
	if s.Config.OIDCissuer == "" {
		return nil, nil, authFailure(ErrInvalid, "oidc_discovery", "provider_disabled")
	}
	trace := tracedClient(s.HTTP)
	p, e := oidc.NewProvider(oidc.ClientContext(ctx, trace.client), s.Config.OIDCissuer)
	if e != nil {
		return nil, trace, trace.failure(ErrUpstream, "oidc_discovery", "invalid_discovery", e)
	}
	return p, trace, nil
}
func (s *Service) oidcConfig(p *oidc.Provider) oauth2.Config {
	return oauth2.Config{ClientID: s.Config.OIDCclientID, ClientSecret: s.Config.OIDCsecret, Endpoint: p.Endpoint(), RedirectURL: s.Config.PublicURL + "/api/v1/user/oauth/callback/" + s.Config.OIDCProviderID(), Scopes: []string{oidc.ScopeOpenID, "profile"}}
}

func (s *Service) BeginFlow(ctx context.Context, provider, intent string, session *Session, returnTo string) (result FlowStart, err error) {
	started := time.Now()
	defer func() {
		if err != nil {
			err = stageFailure(err, "flow_start")
			s.logAuthFailure(ctx, provider, err, started)
		}
	}()
	if provider != "qq" && provider != s.Config.OIDCProviderID() || intent != "login" && intent != "bind" {
		return FlowStart{}, ErrInvalid
	}
	if intent == "bind" && session == nil {
		return FlowStart{}, ErrUnauthorized
	}
	state, cookie, nonce := RandomToken(), RandomToken(), RandomToken()
	id := storage.NewID()
	ctx = withFlowDiagnostic(ctx, id.String())
	payload := flowSecret{Verifier: oauth2.GenerateVerifier(), ReturnTo: validReturn(returnTo)}
	var authURL string
	if provider == s.Config.OIDCProviderID() {
		p, _, e := s.provider(ctx)
		if e != nil {
			return FlowStart{}, e
		}
		c := s.oidcConfig(p)
		authURL = c.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(payload.Verifier))
	} else {
		if s.Config.QQappID == "" {
			return FlowStart{}, ErrInvalid
		}
		u, _ := url.Parse("https://graph.qq.com/oauth2.0/authorize")
		q := url.Values{"response_type": {"code"}, "client_id": {s.Config.QQappID}, "redirect_uri": {s.Config.PublicURL + "/api/v1/user/oauth/callback/qq"}, "state": {state}, "scope": {"get_user_info"}}
		u.RawQuery = q.Encode()
		authURL = u.String()
	}
	b, e := s.seal(payload)
	if e != nil {
		return FlowStart{}, e
	}
	var uid, sid any
	if session != nil {
		uid, _ = storage.ParseID(session.UserID)
		sid, _ = storage.ParseID(session.ID)
	}
	now := s.Now()
	var family browserFamily
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		families := browserFamilies(ctx)
		if sid != nil {
			var existing []byte
			if err := tx.QueryRowContext(ctx, "SELECT browser_family_id FROM sessions WHERE id=?", sid).Scan(&existing); err != nil {
				return err
			}
			families = append(families, storage.IDString(existing))
		}
		var err error
		family, err = s.mergeFamiliesTx(ctx, tx, families, true)
		if err != nil {
			return err
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO auth_flows(id,state_hash,cookie_hash,provider,intent,user_id,session_id,nonce,secret,created_at,expires_at,browser_family_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id[:], hash(state), hash(cookie), provider, intent, uid, sid, nonce, b, now.Unix(), now.Add(10*time.Minute).Unix(), family.id)
		return e
	})
	if e == nil {
		s.logAuth(ctx, slog.LevelInfo, provider, "flow_start", "created", started, "intent", intent)
	}
	return FlowStart{URL: authURL, State: state, Cookie: id.String() + "." + cookie, BrowserFamily: storage.IDString(family.id)}, e
}
func (s *Service) readFlow(ctx context.Context, tx *sql.Tx, cookie string) (flow, error) {
	var out flow
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 {
		return out, authFailure(ErrUnauthorized, "flow_read", "invalid_cookie")
	}
	id, e := storage.ParseID(parts[0])
	if e != nil || len(parts[1]) != 43 {
		return out, authFailure(ErrUnauthorized, "flow_read", "invalid_cookie")
	}
	var secret, sid []byte
	var consumed sql.NullInt64
	const query = "SELECT id,provider,intent,nonce,secret,session_id,stage,attempts,expires_at,consumed_at FROM auth_flows WHERE id=? AND cookie_hash=?"
	var row *sql.Row
	if tx != nil {
		row = tx.QueryRowContext(ctx, query, id, hash(parts[1]))
	} else {
		row = s.Store.DB.QueryRowContext(ctx, query, id, hash(parts[1]))
	}
	e = row.Scan(&out.ID, &out.Provider, &out.Intent, &out.Nonce, &secret, &sid, &out.Stage, &out.Attempts, &out.Expires, &consumed)
	if errors.Is(e, sql.ErrNoRows) {
		return out, authFailure(ErrUnauthorized, "flow_read", "flow_not_found")
	}
	if e != nil {
		return out, stageFailure(e, "flow_read")
	}
	if out.Expires <= s.Now().Unix() {
		return out, authFailure(ErrUnauthorized, "flow_read", "flow_expired")
	}
	if consumed.Valid {
		return out, authFailure(ErrUnauthorized, "flow_read", "flow_consumed")
	}
	out.SessionID = storage.IDString(sid)
	out.Secret, e = s.unseal(secret)
	return out, stageFailure(e, "flow_decrypt")
}
func (s *Service) claimFlow(ctx context.Context, state, cookie string) (flow, error) {
	var out flow
	e := s.Store.Write(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = s.readFlow(ctx, tx, cookie)
		if e != nil {
			return e
		}
		if out.Stage != "pending" {
			return authFailure(ErrUnauthorized, "flow_claim", "flow_already_claimed")
		}
		r, e := tx.ExecContext(ctx, "UPDATE auth_flows SET stage='processing' WHERE id=? AND state_hash=? AND stage='pending'", out.ID, hash(state))
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return authFailure(ErrUnauthorized, "flow_claim", "state_mismatch")
		}
		return nil
	})
	return out, stageFailure(e, "flow_claim")
}
func (s *Service) FlowStatus(ctx context.Context, cookie string) (FlowStatus, error) {
	f, e := s.readFlow(ctx, nil, cookie)
	return FlowStatus{f.Stage, f.Provider, f.Secret.ReturnTo}, e
}

func (s *Service) Callback(ctx context.Context, provider, state, code, cookie string, current *Session) (result FlowResult, err error) {
	started := time.Now()
	ctx = withFlowDiagnostic(ctx, cookie)
	defer func() {
		if err != nil {
			err = stageFailure(err, "callback")
			s.logAuthFailure(ctx, provider, err, started)
			return
		}
		fields := []any{}
		if result.Credentials != nil {
			fields = append(fields, "user_id", result.Credentials.Session.UserID, "identity_id", result.Credentials.Session.IdentityID)
		} else if current != nil {
			fields = append(fields, "user_id", current.UserID, "identity_id", current.IdentityID)
		}
		outcome := "completed"
		if result.NeedQQ {
			outcome = "qq_number_required"
		}
		s.logAuth(ctx, slog.LevelInfo, provider, "callback", outcome, started, fields...)
	}()
	if provider != "qq" && provider != s.Config.OIDCProviderID() {
		return FlowResult{}, ErrInvalid
	}
	f, e := s.claimFlow(ctx, state, cookie)
	if e != nil {
		return FlowResult{}, e
	}
	if f.Provider != provider {
		return FlowResult{}, authFailure(ErrInvalid, "callback_validate", "provider_mismatch")
	}
	if code == "" {
		return FlowResult{}, authFailure(ErrInvalid, "callback_validate", "authorization_code_missing")
	}
	if f.Intent == "bind" && (current == nil || current.ID != f.SessionID) {
		return FlowResult{}, authFailure(ErrUnauthorized, "callback_validate", "binding_session_mismatch")
	}
	var p Identity
	if provider == s.Config.OIDCProviderID() {
		p, e = s.exchangeOIDC(ctx, code, f)
	} else {
		p, e = s.exchangeQQ(ctx, code)
	}
	if e != nil {
		return FlowResult{}, e
	}
	if provider == "qq" {
		known, lookupErr := lookupIdentity(ctx, s.Store.DB, p)
		e = lookupErr
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return FlowResult{}, e
		}
		p.QQNumber = known.qqNumber
		if p.QQNumber == "" {
			f.Secret.Identity = secretIdentity(p)
			b, e := s.seal(f.Secret)
			if e != nil {
				return FlowResult{}, e
			}
			e = s.Store.Write(ctx, func(tx *sql.Tx) error {
				r, e := tx.ExecContext(ctx, "UPDATE auth_flows SET stage='qq_number',secret=? WHERE id=? AND stage='processing' AND consumed_at IS NULL AND expires_at>?", b, f.ID, s.Now().Unix())
				if e != nil {
					return e
				}
				n, e := r.RowsAffected()
				if e != nil {
					return e
				}
				if n != 1 {
					return ErrUnauthorized
				}
				return nil
			})
			return FlowResult{ReturnTo: f.Secret.ReturnTo, NeedQQ: true}, e
		}
	}
	return s.finishFlow(ctx, f, p)
}
func (s *Service) exchangeOIDC(ctx context.Context, code string, f flow) (Identity, error) {
	p, trace, e := s.provider(ctx)
	if e != nil {
		return Identity{}, e
	}
	c := s.oidcConfig(p)
	ctx = oidc.ClientContext(ctx, trace.client)
	trace.reset()
	token, e := c.Exchange(ctx, code, oauth2.VerifierOption(f.Secret.Verifier))
	if e != nil {
		return Identity{}, trace.failure(ErrUpstream, "oidc_token", "invalid_token_response", e)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Identity{}, authFailure(ErrUnauthorized, "oidc_token", "id_token_missing")
	}
	trace.reset()
	verified, e := p.Verifier(&oidc.Config{ClientID: c.ClientID}).Verify(ctx, raw)
	if e != nil {
		return Identity{}, trace.failure(ErrUnauthorized, "oidc_verify", "token_verification_failed", e)
	}
	if verified.Nonce != f.Nonce {
		return Identity{}, authFailure(ErrUnauthorized, "oidc_verify", "nonce_mismatch")
	}
	var claims oidcProfileClaims
	if e = verified.Claims(&claims); e != nil {
		return Identity{}, authFailure(ErrUnauthorized, "oidc_claims", "invalid_claims")
	}
	var additional oidcProfileClaims
	userInfoStatus := "not_needed"
	tokenPicture, _ := safeOIDCAvatar(claims.Picture)
	if claims.Name == "" && claims.Preferred == "" || tokenPicture == "" {
		trace.reset()
		infoStarted := time.Now()
		info, err := p.UserInfo(ctx, oauth2.StaticTokenSource(token))
		switch {
		case err != nil:
			userInfoStatus = "unavailable"
			s.logAuthFailure(ctx, s.Config.OIDCProviderID(), trace.failure(ErrUpstream, "oidc_userinfo", "invalid_userinfo", err), infoStarted)
		case info.Subject != verified.Subject:
			userInfoStatus = "subject_mismatch"
			s.logAuthFailure(ctx, s.Config.OIDCProviderID(), authFailure(ErrUnauthorized, "oidc_userinfo", "subject_mismatch"), infoStarted)
		default:
			var enriched oidcProfileClaims
			if err := info.Claims(&enriched); err != nil {
				userInfoStatus = "invalid_claims"
				s.logAuthFailure(ctx, s.Config.OIDCProviderID(), authFailure(ErrUnauthorized, "oidc_userinfo", "invalid_claims"), infoStarted)
			} else {
				userInfoStatus, additional = "accepted", enriched
				// UserInfo only enriches this verified subject; it cannot replace
				// identity keys or a name already supplied by the ID token.
				if claims.Name == "" && claims.Preferred == "" {
					claims.Name, claims.Preferred = additional.Name, additional.Preferred
				}
			}
		}
	}
	// Prefer usable standard picture claims before the Casdoor avatar alias.
	avatar, source, rejected := selectOIDCAvatar(
		oidcAvatarClaim{source: "id_token.picture", value: claims.Picture},
		oidcAvatarClaim{source: "userinfo.picture", value: additional.Picture},
		claims.avatarClaim("id_token"), additional.avatarClaim("userinfo"),
	)
	status, reason := "accepted", ""
	level := slog.LevelInfo
	if avatar == "" {
		status, reason = "missing", "avatar_absent"
		if userInfoStatus != "accepted" && userInfoStatus != "not_needed" {
			reason = "userinfo_" + userInfoStatus
		}
	}
	if rejected != "" {
		level = slog.LevelWarn
		if avatar == "" {
			reason = "avatar_rejected"
		}
	}
	if userInfoStatus == "subject_mismatch" || userInfoStatus == "invalid_claims" || avatar == "" && userInfoStatus == "unavailable" {
		level = slog.LevelWarn
	}
	// Only fixed diagnostic values and the verified identity are logged, never
	// upstream errors (which can contain response bodies), tokens, or avatar URLs.
	unavailable := userInfoStatus != "accepted" && userInfoStatus != "not_needed"
	_, avatarHost := avatarDiagnostics(avatar)
	diagnostic, _ := ctx.Value(diagnosticContextKey{}).(diagnosticContext)
	s.Log.Log(ctx, level, fmt.Sprintf("请求 %s，流程 %s，OIDC 提供方 %s，已验证 subject %s：头像 %s，来源 %s，原因 %s，拒绝 %s，UserInfo %s", diagnostic.requestID, diagnostic.flowID, s.Config.OIDCProviderID(), verified.Subject, status, source, reason, rejected, userInfoStatus),
		"request_id", diagnostic.requestID, "flow_id", diagnostic.flowID,
		"provider_id", s.Config.OIDCProviderID(), "subject", verified.Subject,
		"avatar_status", status, "avatar_source", source, "avatar_reason", reason, "avatar_rejections", rejected, "userinfo_status", userInfoStatus,
		"avatar_host", avatarHost, "avatar_fallback", avatar != "" && source != "id_token.picture", "avatar_preserve_existing", unavailable && avatar == "",
		"id_token_picture_present", claims.Picture != "", "id_token_avatar_present", len(claims.Avatar) > 0,
		"userinfo_picture_present", additional.Picture != "", "userinfo_avatar_present", len(additional.Avatar) > 0)
	if claims.Name == "" {
		claims.Name = claims.Preferred
	}
	return Identity{
		Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: verified.Subject,
		Nickname: claims.Name, AvatarURL: avatar,
		preserveNickname: unavailable && claims.Name == "",
		preserveAvatar:   unavailable && avatar == "",
	}, nil
}
func (s *Service) finishFlow(ctx context.Context, f flow, p Identity) (FlowResult, error) {
	// A one-way stage claim prevents replay even if the process stops before sending the cookie.
	e := s.Store.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE auth_flows SET stage='finishing' WHERE id=? AND stage IN ('processing','qq_number') AND consumed_at IS NULL AND expires_at>?", f.ID, s.Now().Unix())
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrUnauthorized
		}
		return nil
	})
	if e != nil {
		return FlowResult{}, stageFailure(e, "flow_finish")
	}
	out := FlowResult{ReturnTo: f.Secret.ReturnTo}
	if f.Intent == "bind" {
		e = s.bindIdentity(ctx, f.SessionID, p, f.ID)
	} else {
		var c Credentials
		c, e = s.loginIdentity(ctx, p, f.ID)
		if e == nil {
			out.Credentials = &c
		}
	}
	if e != nil {
		return FlowResult{}, e
	}
	return out, e
}
func (s *Service) CompleteQQ(ctx context.Context, cookie, number string, current *Session) (result FlowResult, err error) {
	started := time.Now()
	ctx = withFlowDiagnostic(ctx, cookie)
	defer func() {
		if err != nil {
			err = stageFailure(err, "qq_confirm")
			s.logAuthFailure(ctx, "qq", err, started)
			return
		}
		fields := []any{}
		if result.Credentials != nil {
			fields = append(fields, "user_id", result.Credentials.Session.UserID, "identity_id", result.Credentials.Session.IdentityID)
		}
		s.logAuth(ctx, slog.LevelInfo, "qq", "qq_confirm", "completed", started, fields...)
	}()
	if !validQQ(number) {
		return FlowResult{}, authFailure(ErrInvalid, "qq_confirm", "invalid_number_format")
	}
	f, e := s.readFlow(ctx, nil, cookie)
	if e != nil {
		return FlowResult{}, e
	}
	if f.Stage != "qq_number" {
		return FlowResult{}, authFailure(ErrInvalid, "qq_confirm", "invalid_flow_stage")
	}
	if f.Attempts >= 5 {
		return FlowResult{}, authFailure(ErrInvalid, "qq_confirm", "attempt_limit")
	}
	if f.Intent == "bind" && (current == nil || current.ID != f.SessionID) {
		return FlowResult{}, authFailure(ErrUnauthorized, "qq_confirm", "binding_session_mismatch")
	}
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE auth_flows SET attempts=attempts+1 WHERE id=? AND stage='qq_number' AND attempts<5 AND consumed_at IS NULL", f.ID)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrInvalid
		}
		return nil
	})
	if e != nil {
		return FlowResult{}, e
	}
	p := f.Secret.Identity.profile()
	if e = s.checkQQAvatar(ctx, p.AvatarURL, number); e != nil {
		return FlowResult{}, e
	}
	p.QQNumber = number
	return s.finishFlow(ctx, f, p)
}
func validQQ(v string) bool {
	if len(v) < 5 || len(v) > 12 || v[0] == '0' {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
