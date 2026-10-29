package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/disintegration/imaging"
)

func (s *Service) getQQ(ctx context.Context, path string, q url.Values, out any) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.qq.com/"+path+"?"+q.Encode(), nil)
	if e != nil {
		return ErrInvalid
	}
	r, e := s.HTTP.Do(req)
	if e != nil {
		return ErrUpstream
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return ErrUpstream
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 128*1024+1))
	if e != nil || len(b) > 128*1024 {
		return ErrUpstream
	}
	b = bytes.TrimSpace(b)
	if bytes.HasPrefix(b, []byte("callback")) {
		start, end := bytes.IndexByte(b, '('), bytes.LastIndexByte(b, ')')
		if start < 0 || end <= start {
			return ErrUpstream
		}
		b = bytes.TrimSpace(b[start+1 : end])
	}
	if e = json.Unmarshal(b, out); e != nil {
		return ErrUpstream
	}
	return nil
}
func (s *Service) exchangeQQ(ctx context.Context, code string) (Identity, error) {
	var token struct {
		Token string `json:"access_token"`
		Error int    `json:"error"`
	}
	e := s.getQQ(ctx, "oauth2.0/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {s.Config.QQappID}, "client_secret": {s.Config.QQsecret}, "code": {code}, "redirect_uri": {s.Config.PublicURL + "/api/v1/user/oauth/callback/qq"}, "fmt": {"json"}}, &token)
	if e != nil {
		return Identity{}, e
	}
	if token.Token == "" || token.Error != 0 {
		return Identity{}, ErrUnauthorized
	}
	var me struct {
		ClientID string `json:"client_id"`
		OpenID   string `json:"openid"`
	}
	if e = s.getQQ(ctx, "oauth2.0/me", url.Values{"access_token": {token.Token}, "fmt": {"json"}}, &me); e != nil {
		return Identity{}, e
	}
	if me.ClientID != s.Config.QQappID || me.OpenID == "" {
		return Identity{}, ErrUnauthorized
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
		return Identity{}, ErrUnauthorized
	}
	if info.Avatar == "" {
		info.Avatar = info.AvatarSmall
	}
	if strings.HasPrefix(info.Avatar, "http://") {
		info.Avatar = "https://" + strings.TrimPrefix(info.Avatar, "http://")
	}
	return Identity{Provider: "qq", Issuer: s.Config.QQappID, Subject: me.OpenID, Nickname: info.Nickname, AvatarURL: SafeAvatar(info.Avatar)}, nil
}
func (s *Service) avatar(ctx context.Context, raw string) (image.Image, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || !(u.Hostname() == "qlogo.cn" || strings.HasSuffix(u.Hostname(), ".qlogo.cn")) || u.Port() != "" {
		return nil, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return nil, ErrInvalid
	}
	r, e := s.HTTP.Do(req)
	if e != nil {
		return nil, ErrUpstream
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, ErrUpstream
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
	if e != nil || len(b) > 1024*1024 {
		return nil, ErrUpstream
	}
	c, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || c.Width > 2048 || c.Height > 2048 || c.Width < 1 || c.Height < 1 {
		return nil, ErrUpstream
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return nil, ErrUpstream
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
		return ErrInvalid
	}
	return nil
}
