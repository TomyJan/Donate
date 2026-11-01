package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/storage"
)

var (
	ErrUnauthorized = errors.New("登录状态无效")
	ErrBanned       = errors.New("账号已封禁")
	ErrConflict     = errors.New("该身份已绑定账号或当前账号已绑定此登录方式")
	ErrInvalid      = errors.New("参数无效")
	ErrUpstream     = errors.New("第三方服务暂不可用")
)

const (
	IdleTTL     = 30 * 24 * time.Hour
	RenewWindow = 20 * 24 * time.Hour
)

type Identity struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Issuer    string    `json:"-"`
	Subject   string    `json:"-"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url"`
	QQNumber  string    `json:"-"`
	QQOpenID  string    `json:"-"`
	QQUnionID string    `json:"-"`
	BoundAt   time.Time `json:"bound_at"`

	// Failed enrichment must not erase fields from an existing identity.
	preserveNickname bool
	preserveAvatar   bool
}
type Session struct {
	ID, UserID, IdentityID, Nickname, AvatarURL, Provider, CSRF string
	ExpiresAt                                                   time.Time
	Admin                                                       bool
	BrowserFamily                                               string
}
type Credentials struct {
	Token   string
	Session Session
}
type Profile struct {
	ID        string     `json:"id"`
	Nickname  string     `json:"nickname"`
	AvatarURL string     `json:"avatar_url"`
	Provider  string     `json:"provider"`
	Admin     bool       `json:"admin"`
	CSRF      string     `json:"csrf_token"`
	Bindings  []Identity `json:"bindings"`
	ExpiresAt time.Time  `json:"expires_at"`
}
type Service struct {
	Store  *storage.Store
	Config config.Config
	Log    *slog.Logger
	Now    func() time.Time
	HTTP   *http.Client
}

func New(s *storage.Store, c config.Config, l *slog.Logger) *Service {
	return &Service{Store: s, Config: c, Log: l, Now: time.Now, HTTP: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func RandomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hash(s string) []byte { v := sha256.Sum256([]byte(s)); return v[:] }
func csrf(token string) string {
	h := hmac.New(sha256.New, []byte(token))
	h.Write([]byte("donate/csrf/v1"))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func SafeAvatar(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return u.String()
}

func (s *Service) LoginIdentity(ctx context.Context, p Identity) (Credentials, error) {
	started := time.Now()
	c, err := s.loginIdentity(ctx, p, nil)
	if err != nil {
		s.logAuthFailure(ctx, p.Provider, err, started)
	}
	return c, err
}
func (s *Service) loginIdentity(ctx context.Context, p Identity, flowID []byte) (result Credentials, failure error) {
	started := time.Now()
	defer func() { failure = stageFailure(failure, "session_create") }()
	var validation error
	p, validation = s.normalizeIdentity(p)
	if validation != nil {
		return Credentials{}, validation
	}
	now := s.Now().UTC()
	sid := storage.NewID()
	token := RandomToken()
	var uid, iid []byte
	err := s.Store.Write(ctx, func(tx *sql.Tx) error {
		families := browserFamilies(ctx)
		if len(flowID) != 0 {
			var family []byte
			if e := tx.QueryRowContext(ctx, "SELECT browser_family_id FROM auth_flows WHERE id=?", flowID).Scan(&family); e != nil {
				return e
			}
			if len(family) != 0 {
				families = append(families, storage.IDString(family))
			}
		}
		family, e := s.mergeFamiliesTx(ctx, tx, families, true)
		if e != nil {
			return e
		}
		var issued int64
		if e := tx.QueryRowContext(ctx, "UPDATE browser_session_sequence SET issued_order=issued_order+1 WHERE id=1 RETURNING issued_order").Scan(&issued); e != nil {
			return e
		}
		matched, e := lookupIdentity(ctx, tx, p)
		iid, uid = matched.id, matched.userID
		if e == nil && matched.status != "active" {
			return ErrBanned
		}
		if errors.Is(e, sql.ErrNoRows) {
			u, i := storage.NewID(), storage.NewID()
			uid, iid = u[:], i[:]
			if _, e = tx.ExecContext(ctx, "INSERT INTO users(id,created_at,last_login_at) VALUES(?,?,?)", uid, now.Unix(), now.Unix()); e != nil {
				return e
			}
			if e = insertIdentity(ctx, tx, iid, uid, p, now); e != nil {
				return e
			}
		} else if e != nil {
			return e
		} else {
			if _, e = tx.ExecContext(ctx, "UPDATE identities SET subject=?,nickname=CASE WHEN ? THEN nickname ELSE ? END,avatar_url=CASE WHEN ? THEN avatar_url ELSE ? END,qq_openid=CASE WHEN ?='' THEN qq_openid ELSE ? END,qq_unionid=CASE WHEN ?='' THEN qq_unionid ELSE ? END,updated_at=? WHERE id=?", p.Subject, p.preserveNickname, name(p.Nickname), p.preserveAvatar, SafeAvatar(p.AvatarURL), p.QQOpenID, p.QQOpenID, p.QQUnionID, p.QQUnionID, now.Unix(), iid); e != nil {
				if storage.Conflict(e) {
					return ErrConflict
				}
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, "UPDATE users SET public_identity_id=?,last_login_at=? WHERE id=?", iid, now.Unix(), uid); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO sessions(id,user_id,identity_id,token_hash,csrf_hash,created_at,renewed_at,expires_at,browser_family_id,issued_order) VALUES(?,?,?,?,?,?,?,?,?,?)", sid[:], uid, iid, hash(token), hash(csrf(token)), now.Unix(), now.Unix(), now.Add(IdleTTL).Unix(), family.id, issued)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE browser_families SET current_session_id=?,current_issued_order=? WHERE id=?", sid[:], issued, family.id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=COALESCE(revoked_at,?) WHERE browser_family_id=? AND id<>?", now.Unix(), family.id, sid[:]); e != nil {
			return e
		}
		return s.consumeFlowTx(ctx, tx, flowID, true)
	})
	if err != nil {
		return Credentials{}, err
	}
	session, err := s.Resolve(ctx, token)
	if err == nil {
		fields := append(s.avatarLogFields(session), "session_id", session.ID, "provider", p.Provider, "avatar_preserved", p.preserveAvatar && session.AvatarURL != "")
		s.logAuth(ctx, slog.LevelInfo, p.Provider, "session_create", "completed", started, fields...)
	}
	return Credentials{token, session}, err
}
func name(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "捐赠者"
	}
	r := []rune(v)
	if len(r) > 80 {
		return string(r[:80])
	}
	return v
}
func insertIdentity(ctx context.Context, tx *sql.Tx, iid, uid []byte, p Identity, now time.Time) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO identities(id,user_id,provider,issuer,subject,nickname,avatar_url,qq_number,qq_openid,qq_unionid,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", iid, uid, p.Provider, p.Issuer, p.Subject, name(p.Nickname), SafeAvatar(p.AvatarURL), p.QQNumber, p.QQOpenID, p.QQUnionID, now.Unix(), now.Unix())
	if storage.Conflict(e) {
		return ErrConflict
	}
	return e
}

func (s *Service) Resolve(ctx context.Context, token string) (out Session, failure error) {
	defer func() { failure = stageFailure(failure, "session_resolve") }()
	if len(token) != 43 {
		return out, authFailure(ErrUnauthorized, "session_resolve", "invalid_session_token")
	}
	var sid, uid, iid, family []byte
	var expires int64
	var status string
	var revoked sql.NullInt64
	e := s.Store.DB.QueryRowContext(ctx, "SELECT s.id,s.user_id,s.identity_id,s.expires_at,s.revoked_at,u.status,i.nickname,i.avatar_url,i.provider,s.browser_family_id FROM sessions s JOIN users u ON u.id=s.user_id JOIN identities i ON i.id=s.identity_id WHERE s.token_hash=?", hash(token)).Scan(&sid, &uid, &iid, &expires, &revoked, &status, &out.Nickname, &out.AvatarURL, &out.Provider, &family)
	if errors.Is(e, sql.ErrNoRows) {
		return out, authFailure(ErrUnauthorized, "session_resolve", "session_not_found")
	}
	if e != nil {
		return out, e
	}
	if status != "active" {
		return out, authFailure(ErrBanned, "session_resolve", "account_banned")
	}
	if revoked.Valid {
		return out, authFailure(ErrUnauthorized, "session_resolve", "session_revoked")
	}
	if expires <= s.Now().Unix() {
		return out, authFailure(ErrUnauthorized, "session_resolve", "session_expired")
	}
	current, e := currentBrowserSession(ctx, s.Store.DB, family, sid)
	if e != nil {
		return out, e
	}
	if !current {
		return out, authFailure(ErrUnauthorized, "session_resolve", "session_superseded")
	}
	root, e := familyRoot(ctx, s.Store.DB, family)
	if e != nil {
		return out, e
	}
	out.BrowserFamily = storage.IDString(root.id)
	out.ID, out.UserID, out.IdentityID = storage.IDString(sid), storage.IDString(uid), storage.IDString(iid)
	out.ExpiresAt = time.Unix(expires, 0).UTC()
	out.CSRF = csrf(token)
	out.Admin = s.Config.AdminIDs[out.UserID]
	return out, nil
}
func (s *Service) Renew(ctx context.Context, token string) (Session, error) {
	session, _, err := s.RenewIfNeeded(ctx, token)
	return session, err
}

// RenewIfNeeded reports whether this call extended the session's expiry.
func (s *Service) RenewIfNeeded(ctx context.Context, token string) (result Session, didRenew bool, failure error) {
	started := time.Now()
	defer func() { failure = stageFailure(failure, "session_renew") }()
	session, e := s.Resolve(ctx, token)
	if e != nil {
		return Session{}, false, e
	}
	if session.ExpiresAt.Sub(s.Now()) > RenewWindow {
		return session, false, nil
	}
	renewed := false
	var sid []byte
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		now := s.Now().UTC()
		var uid, family []byte
		var expires int64
		var revoked sql.NullInt64
		var status string
		e := tx.QueryRowContext(ctx, "SELECT s.id,s.user_id,s.expires_at,s.revoked_at,u.status,s.browser_family_id FROM sessions s JOIN users u ON u.id=s.user_id WHERE token_hash=?", hash(token)).Scan(&sid, &uid, &expires, &revoked, &status, &family)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrUnauthorized
		}
		if e != nil {
			return e
		}
		if status != "active" {
			return ErrBanned
		}
		if revoked.Valid || expires <= now.Unix() {
			return ErrUnauthorized
		}
		current, e := currentBrowserSession(ctx, tx, family, sid)
		if e != nil {
			return e
		}
		if !current {
			return ErrUnauthorized
		}
		// Another request may already have renewed while this transaction waited.
		if time.Unix(expires, 0).Sub(now) > RenewWindow {
			return nil
		}
		_, e = tx.ExecContext(ctx, "UPDATE sessions SET renewed_at=?,expires_at=? WHERE id=? AND revoked_at IS NULL", now.Unix(), now.Add(IdleTTL).Unix(), sid)
		renewed = e == nil
		return e
	})
	if e != nil {
		return Session{}, false, e
	}
	session, e = s.Resolve(ctx, token)
	if e == nil && renewed {
		d, _ := ctx.Value(diagnosticContextKey{}).(diagnosticContext)
		s.Log.InfoContext(ctx, fmt.Sprintf("请求 %s，用户 %s 的会话 %s 已续期至 %s", d.requestID, session.UserID, session.ID, session.ExpiresAt.Format(time.RFC3339)), "request_id", d.requestID, "stage", "session_renew", "user_id", session.UserID, "session_id", session.ID, "identity_id", session.IdentityID, "expires_at", session.ExpiresAt, "duration_ms", time.Since(started).Milliseconds())
	}
	return session, renewed, e
}
func (s *Service) Logout(ctx context.Context, token string) (failure error) {
	started := time.Now()
	defer func() { failure = stageFailure(failure, "session_logout") }()
	session, e := s.Resolve(ctx, token)
	if e != nil {
		return e
	}
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL", s.Now().Unix(), hash(token))
		return e
	})
	if e == nil {
		s.logAuth(ctx, slog.LevelInfo, session.Provider, "session_logout", "completed", started, "user_id", session.UserID, "session_id", session.ID, "identity_id", session.IdentityID)
	}
	return e
}
func (s *Service) Profile(ctx context.Context, session Session) (out Profile, failure error) {
	started := time.Now()
	defer func() { failure = stageFailure(failure, "profile_read") }()
	out = Profile{ID: session.UserID, Nickname: session.Nickname, AvatarURL: session.AvatarURL, Provider: session.Provider, Admin: session.Admin, CSRF: session.CSRF, ExpiresAt: session.ExpiresAt, Bindings: []Identity{}}
	uid, _ := storage.ParseID(session.UserID)
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,provider,nickname,avatar_url,created_at FROM identities WHERE user_id=? ORDER BY provider", uid)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var p Identity
		var id []byte
		var created int64
		if e = rows.Scan(&id, &p.Provider, &p.Nickname, &p.AvatarURL, &created); e != nil {
			return out, e
		}
		p.ID = storage.IDString(id)
		p.BoundAt = time.Unix(created, 0).UTC()
		out.Bindings = append(out.Bindings, p)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if s.Log.Enabled(ctx, slog.LevelDebug) {
		s.logAuth(ctx, slog.LevelDebug, session.Provider, "profile", "read", started, s.avatarLogFields(session)...)
	}
	return out, nil
}
func (s *Service) BindIdentity(ctx context.Context, sessionID string, p Identity) error {
	started := time.Now()
	err := s.bindIdentity(ctx, sessionID, p, nil)
	if err != nil {
		s.logAuthFailure(ctx, p.Provider, err, started)
	}
	return err
}
func (s *Service) bindIdentity(ctx context.Context, sessionID string, p Identity, flowID []byte) (failure error) {
	started := time.Now()
	defer func() { failure = stageFailure(failure, "identity_bind") }()
	var validation error
	p, validation = s.normalizeIdentity(p)
	if validation != nil {
		return validation
	}
	sid, e := storage.ParseID(sessionID)
	if e != nil {
		return ErrUnauthorized
	}
	now := s.Now()
	var uid, identityID []byte
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		var status string
		var expires int64
		var revoked sql.NullInt64
		var family []byte
		e := tx.QueryRowContext(ctx, "SELECT s.user_id,s.expires_at,s.revoked_at,u.status,s.browser_family_id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=?", sid).Scan(&uid, &expires, &revoked, &status, &family)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrUnauthorized
		}
		if e != nil {
			return e
		}
		if status != "active" {
			return ErrBanned
		}
		if revoked.Valid || expires <= now.Unix() {
			return ErrUnauthorized
		}
		current, e := currentBrowserSession(ctx, tx, family, sid)
		if e != nil {
			return e
		}
		if !current {
			return ErrUnauthorized
		}
		if _, lookupErr := lookupIdentity(ctx, tx, p); !errors.Is(lookupErr, sql.ErrNoRows) {
			if lookupErr == nil {
				return ErrConflict
			}
			return lookupErr
		}
		id := storage.NewID()
		identityID = id[:]
		if e = insertIdentity(ctx, tx, id[:], uid, p, now); e != nil {
			return e
		}
		return s.consumeFlowTx(ctx, tx, flowID, false)
	})
	if e == nil {
		fields := s.avatarLogFields(Session{UserID: storage.IDString(uid), IdentityID: storage.IDString(identityID), Provider: p.Provider, AvatarURL: SafeAvatar(p.AvatarURL)})
		fields = append(fields, "session_id", sessionID)
		s.logAuth(ctx, slog.LevelInfo, p.Provider, "identity_bind", "completed", started, fields...)
	}
	return e
}

func (s *Service) consumeFlowTx(ctx context.Context, tx *sql.Tx, id []byte, replace bool) error {
	if len(id) == 0 {
		return nil
	}
	if replace {
		if _, e := tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE id=(SELECT session_id FROM auth_flows WHERE id=?) AND revoked_at IS NULL", s.Now().Unix(), id); e != nil {
			return e
		}
	}
	r, e := tx.ExecContext(ctx, "UPDATE auth_flows SET stage='done',consumed_at=?,secret=x'' WHERE id=? AND stage='finishing' AND consumed_at IS NULL AND expires_at>?", s.Now().Unix(), id, s.Now().Unix())
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrUnauthorized
	}
	return nil
}
func (s *Service) CheckCSRF(token, provided string) bool {
	if len(provided) != 43 {
		return false
	}
	return hmac.Equal([]byte(csrf(token)), []byte(provided))
}
