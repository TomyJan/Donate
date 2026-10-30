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

const IdleTTL = 30 * 24 * time.Hour

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
}
type Session struct {
	ID, UserID, IdentityID, Nickname, AvatarURL, Provider, CSRF string
	ExpiresAt                                                   time.Time
	Admin                                                       bool
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
	return s.loginIdentity(ctx, p, nil)
}
func (s *Service) loginIdentity(ctx context.Context, p Identity, flowID []byte) (Credentials, error) {
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
			if _, e = tx.ExecContext(ctx, "UPDATE identities SET subject=?,nickname=?,avatar_url=?,qq_openid=CASE WHEN ?='' THEN qq_openid ELSE ? END,qq_unionid=CASE WHEN ?='' THEN qq_unionid ELSE ? END,updated_at=? WHERE id=?", p.Subject, name(p.Nickname), SafeAvatar(p.AvatarURL), p.QQOpenID, p.QQOpenID, p.QQUnionID, p.QQUnionID, now.Unix(), iid); e != nil {
				if storage.Conflict(e) {
					return ErrConflict
				}
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, "UPDATE users SET public_identity_id=?,last_login_at=? WHERE id=?", iid, now.Unix(), uid); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO sessions(id,user_id,identity_id,token_hash,csrf_hash,created_at,renewed_at,expires_at) VALUES(?,?,?,?,?,?,?,?)", sid[:], uid, iid, hash(token), hash(csrf(token)), now.Unix(), now.Unix(), now.Add(IdleTTL).Unix())
		if e != nil {
			return e
		}
		return s.consumeFlowTx(ctx, tx, flowID, true)
	})
	if err != nil {
		return Credentials{}, err
	}
	session, err := s.Resolve(ctx, token)
	if err == nil {
		s.Log.Info(fmt.Sprintf("用户 %s 通过 %s 登录，创建会话 %s", session.UserID, p.Provider, session.ID), "user_id", session.UserID, "session_id", session.ID, "provider", p.Provider)
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

func (s *Service) Resolve(ctx context.Context, token string) (Session, error) {
	var out Session
	if len(token) != 43 {
		return out, ErrUnauthorized
	}
	var sid, uid, iid []byte
	var expires int64
	var status string
	var revoked sql.NullInt64
	e := s.Store.DB.QueryRowContext(ctx, "SELECT s.id,s.user_id,s.identity_id,s.expires_at,s.revoked_at,u.status,i.nickname,i.avatar_url,i.provider FROM sessions s JOIN users u ON u.id=s.user_id JOIN identities i ON i.id=s.identity_id WHERE s.token_hash=?", hash(token)).Scan(&sid, &uid, &iid, &expires, &revoked, &status, &out.Nickname, &out.AvatarURL, &out.Provider)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrUnauthorized
	}
	if e != nil {
		return out, e
	}
	if status != "active" {
		return out, ErrBanned
	}
	if revoked.Valid || expires <= s.Now().Unix() {
		return out, ErrUnauthorized
	}
	out.ID, out.UserID, out.IdentityID = storage.IDString(sid), storage.IDString(uid), storage.IDString(iid)
	out.ExpiresAt = time.Unix(expires, 0).UTC()
	out.CSRF = csrf(token)
	out.Admin = s.Config.AdminIDs[out.UserID]
	return out, nil
}
func (s *Service) Renew(ctx context.Context, token string) (Session, error) {
	now := s.Now().UTC()
	var sid []byte
	e := s.Store.Write(ctx, func(tx *sql.Tx) error {
		var uid []byte
		var expires int64
		var revoked sql.NullInt64
		var status string
		e := tx.QueryRowContext(ctx, "SELECT s.id,s.user_id,s.expires_at,s.revoked_at,u.status FROM sessions s JOIN users u ON u.id=s.user_id WHERE token_hash=?", hash(token)).Scan(&sid, &uid, &expires, &revoked, &status)
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
		_, e = tx.ExecContext(ctx, "UPDATE sessions SET renewed_at=?,expires_at=? WHERE id=? AND revoked_at IS NULL", now.Unix(), now.Add(IdleTTL).Unix(), sid)
		return e
	})
	if e != nil {
		return Session{}, e
	}
	session, e := s.Resolve(ctx, token)
	if e == nil {
		s.Log.Info(fmt.Sprintf("用户 %s 的会话 %s 已续期至 %s", session.UserID, session.ID, session.ExpiresAt.Format(time.RFC3339)), "user_id", session.UserID, "session_id", session.ID, "expires_at", session.ExpiresAt)
	}
	return session, e
}
func (s *Service) Logout(ctx context.Context, token string) error {
	session, e := s.Resolve(ctx, token)
	if e != nil {
		return e
	}
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL", s.Now().Unix(), hash(token))
		return e
	})
	if e == nil {
		s.Log.Info(fmt.Sprintf("用户 %s 已退出，会话 %s 已撤销", session.UserID, session.ID), "user_id", session.UserID, "session_id", session.ID)
	}
	return e
}
func (s *Service) Profile(ctx context.Context, session Session) (Profile, error) {
	out := Profile{ID: session.UserID, Nickname: session.Nickname, AvatarURL: session.AvatarURL, Provider: session.Provider, Admin: session.Admin, CSRF: session.CSRF, ExpiresAt: session.ExpiresAt, Bindings: []Identity{}}
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
	return out, rows.Err()
}
func (s *Service) BindIdentity(ctx context.Context, sessionID string, p Identity) error {
	return s.bindIdentity(ctx, sessionID, p, nil)
}
func (s *Service) bindIdentity(ctx context.Context, sessionID string, p Identity, flowID []byte) error {
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
	var uid []byte
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		var status string
		var expires int64
		var revoked sql.NullInt64
		e := tx.QueryRowContext(ctx, "SELECT s.user_id,s.expires_at,s.revoked_at,u.status FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=?", sid).Scan(&uid, &expires, &revoked, &status)
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
		if _, lookupErr := lookupIdentity(ctx, tx, p); !errors.Is(lookupErr, sql.ErrNoRows) {
			if lookupErr == nil {
				return ErrConflict
			}
			return lookupErr
		}
		id := storage.NewID()
		if e = insertIdentity(ctx, tx, id[:], uid, p, now); e != nil {
			return e
		}
		return s.consumeFlowTx(ctx, tx, flowID, false)
	})
	if e == nil {
		s.Log.Info(fmt.Sprintf("用户 %s 在会话 %s 绑定 %s 身份", storage.IDString(uid), sessionID, p.Provider), "user_id", storage.IDString(uid), "session_id", sessionID, "provider", p.Provider)
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
