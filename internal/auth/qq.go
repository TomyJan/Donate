package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/disintegration/imaging"
)

func (s *Service) getQQ(ctx context.Context, path string, q url.Values, out any) error {
	stage := "qq_profile"
	if path == "oauth2.0/token" {
		stage = "qq_token"
	} else if path == "oauth2.0/me" {
		stage = "qq_identity"
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.qq.com/"+path+"?"+q.Encode(), nil)
	if e != nil {
		return authFailure(ErrInvalid, stage, "invalid_request")
	}
	trace := tracedClient(s.HTTP)
	r, e := trace.client.Do(req)
	if e != nil {
		return trace.failure(ErrUpstream, stage, "request_failed", e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return trace.failure(ErrUpstream, stage, "http_status", nil)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 128*1024+1))
	if e != nil {
		return trace.failure(ErrUpstream, stage, "response_read", e)
	}
	if len(b) > 128*1024 {
		return trace.failure(ErrUpstream, stage, "response_too_large", nil)
	}
	b = bytes.TrimSpace(b)
	if bytes.HasPrefix(b, []byte("callback")) {
		start, end := bytes.IndexByte(b, '('), bytes.LastIndexByte(b, ')')
		if start < 0 || end <= start {
			return trace.failure(ErrUpstream, stage, "invalid_jsonp", nil)
		}
		b = bytes.TrimSpace(b[start+1 : end])
	}
	if e = json.Unmarshal(b, out); e != nil {
		return trace.failure(ErrUpstream, stage, "invalid_json", e)
	}
	return nil
}
func (s *Service) exchangeQQ(ctx context.Context, code string) (Identity, error) {
	started := time.Now()
	var token struct {
		Token string `json:"access_token"`
		Error int    `json:"error"`
	}
	e := s.getQQ(ctx, "oauth2.0/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {s.Config.QQappID}, "client_secret": {s.Config.QQsecret}, "code": {code}, "redirect_uri": {s.Config.PublicURL + "/api/v1/user/oauth/callback/qq"}, "fmt": {"json"}}, &token)
	if e != nil {
		return Identity{}, e
	}
	if token.Token == "" || token.Error != 0 {
		return Identity{}, authFailure(ErrUnauthorized, "qq_token", "token_rejected")
	}
	var me struct {
		ClientID string `json:"client_id"`
		OpenID   string `json:"openid"`
		UnionID  string `json:"unionid"`
		Error    int    `json:"error"`
	}
	identityQuery := url.Values{"access_token": {token.Token}, "fmt": {"json"}}
	if s.Config.QQuseUnionID {
		identityQuery.Set("unionid", "1")
	}
	if e = s.getQQ(ctx, "oauth2.0/me", identityQuery, &me); e != nil {
		return Identity{}, e
	}
	if me.Error != 0 {
		return Identity{}, authFailure(ErrUnauthorized, "qq_identity", "provider_rejected")
	}
	if me.ClientID != s.Config.QQappID {
		return Identity{}, authFailure(ErrUnauthorized, "qq_identity", "client_mismatch")
	}
	if me.OpenID == "" || s.Config.QQuseUnionID && me.UnionID == "" {
		return Identity{}, authFailure(ErrUnauthorized, "qq_identity", "subject_missing")
	}
	var info struct {
		Ret         int    `json:"ret"`
		Nickname    string `json:"nickname"`
		Avatar      string `json:"figureurl_qq_2"`
		AvatarSmall string `json:"figureurl_qq_1"`
	}
	if e = s.getQQ(ctx, "user/get_user_info", url.Values{"access_token": {token.Token}, "oauth_consumer_key": {s.Config.QQappID}, "openid": {me.OpenID}}, &info); e != nil {
		return Identity{}, e
	}
	if info.Ret != 0 {
		return Identity{}, authFailure(ErrUnauthorized, "qq_profile", "provider_rejected")
	}
	source := "figureurl_qq_2"
	if info.Avatar == "" {
		info.Avatar = info.AvatarSmall
		source = "figureurl_qq_1"
	}
	if strings.HasPrefix(info.Avatar, "http://") {
		info.Avatar = "https://" + strings.TrimPrefix(info.Avatar, "http://")
	}
	subject := me.OpenID
	if s.Config.QQuseUnionID {
		subject = me.UnionID
	} else {
		me.UnionID = ""
	}
	avatar := SafeAvatar(info.Avatar)
	status, host := avatarDiagnostics(avatar)
	if info.Avatar != "" && avatar == "" {
		status = "rejected"
	}
	s.logAuth(ctx, slog.LevelInfo, "qq", "qq_profile", "accepted", started, "avatar_source", source, "avatar_status", status, "avatar_present", avatar != "", "avatar_host", host, "avatar_fallback", source == "figureurl_qq_1")
	return Identity{Provider: "qq", Issuer: s.Config.QQappID, Subject: subject, QQOpenID: me.OpenID, QQUnionID: me.UnionID, Nickname: info.Nickname, AvatarURL: avatar}, nil
}
func (s *Service) avatar(ctx context.Context, raw string) (image.Image, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || !(u.Hostname() == "qlogo.cn" || strings.HasSuffix(u.Hostname(), ".qlogo.cn")) || u.Port() != "" {
		return nil, authFailure(ErrInvalid, "qq_avatar_fetch", "avatar_url_rejected")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return nil, authFailure(ErrInvalid, "qq_avatar_fetch", "invalid_request")
	}
	trace := tracedClient(s.HTTP)
	r, e := trace.client.Do(req)
	if e != nil {
		return nil, trace.failure(ErrUpstream, "qq_avatar_fetch", "request_failed", e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, trace.failure(ErrUpstream, "qq_avatar_fetch", "http_status", nil)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
	if e != nil {
		return nil, trace.failure(ErrUpstream, "qq_avatar_fetch", "response_read", e)
	}
	if len(b) > 1024*1024 {
		return nil, trace.failure(ErrUpstream, "qq_avatar_fetch", "response_too_large", nil)
	}
	c, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil {
		return nil, trace.failure(ErrUpstream, "qq_avatar_decode", "image_decode_failed", e)
	}
	if c.Width > 2048 || c.Height > 2048 || c.Width < 1 || c.Height < 1 {
		return nil, trace.failure(ErrUpstream, "qq_avatar_decode", "image_dimensions_rejected", nil)
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return nil, trace.failure(ErrUpstream, "qq_avatar_decode", "image_decode_failed", e)
	}
	return im, nil
}
func (s *Service) checkQQAvatar(ctx context.Context, oauthAvatar, number string) error {
	a, e := s.avatar(ctx, oauthAvatar)
	if e != nil {
		return e
	}
	b, e := s.avatar(ctx, "https://q1.qlogo.cn/g?b=qq&nk="+number+"&s=100")
	if e != nil {
		return e
	}
	a, b = imaging.Resize(a, 64, 64, imaging.Linear), imaging.Resize(b, 64, 64, imaging.Linear)
	var distance float64
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			distance += math.Abs(float64(ar)-float64(br)) + math.Abs(float64(ag)-float64(bg)) + math.Abs(float64(ab)-float64(bb))
		}
	}
	if distance/(64*64*3*65535) > .05 {
		return authFailure(ErrInvalid, "qq_avatar_compare", "image_mismatch")
	}
	return nil
}
