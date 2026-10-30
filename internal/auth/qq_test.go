package auth

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

type qqTransport func(*http.Request) (*http.Response, error)

func (f qqTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestQQOAuthRequiresMatchingNumberAndConsumesFlow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		union bool
	}{{"openid", false}, {"unionid", true}} {
		t.Run(tc.name, func(t *testing.T) { testQQOAuthRequiresMatchingNumberAndConsumesFlow(t, tc.union) })
	}
}
func testQQOAuthRequiresMatchingNumberAndConsumesFlow(t *testing.T, useUnion bool) {
	s := testAuth(t)
	s.Config.QQuseUnionID = useUnion
	s.Config.QQappID = "app"
	s.Config.QQsecret = "secret"
	s.Config.PublicURL = "https://donate.test"
	matching := false
	s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
		body := []byte(`{}`)
		switch r.URL.Path {
		case "/oauth2.0/token":
			body = []byte(`{"access_token":"test-token"}`)
		case "/oauth2.0/me":
			body = []byte(`{"client_id":"app","openid":"open-one","unionid":"union-one"}`)
		case "/user/get_user_info":
			body = []byte(`{"ret":0,"nickname":"QQ User","figureurl_qq_2":"https://q.qlogo.cn/oauth.png"}`)
		default:
			if !strings.HasSuffix(r.URL.Host, "qlogo.cn") {
				t.Fatal("unexpected host", r.URL.Host)
			}
			im := image.NewRGBA(image.Rect(0, 0, 32, 32))
			c := color.RGBA{255, 255, 255, 255}
			if r.URL.Path == "/g" && !matching {
				c = color.RGBA{0, 0, 0, 255}
			}
			for y := range 32 {
				for x := range 32 {
					im.Set(x, y, c)
				}
			}
			var b bytes.Buffer
			if e := png.Encode(&b, im); e != nil {
				t.Fatal(e)
			}
			body = b.Bytes()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
	ctx := context.Background()
	f, e := s.BeginFlow(ctx, "qq", "login", nil, "/")
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Callback(ctx, "qq", f.State, "code", f.Cookie, nil)
	if e != nil || !result.NeedQQ || result.Credentials != nil {
		t.Fatal(result, e)
	}
	if _, e = s.CompleteQQ(ctx, f.Cookie, "123abc", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = s.CompleteQQ(ctx, f.Cookie, "123456", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("mismatched avatar accepted", e)
	}
	matching = true
	result, e = s.CompleteQQ(ctx, f.Cookie, "123456", nil)
	if e != nil || result.Credentials == nil {
		t.Fatal(e)
	}
	profile, e := s.Profile(ctx, result.Credentials.Session)
	if e != nil || profile.Nickname != "QQ User" {
		t.Fatal(profile, e)
	}
	var subject, open, union string
	if e = s.Store.DB.QueryRow("SELECT subject,qq_openid,qq_unionid FROM identities WHERE provider='qq'").Scan(&subject, &open, &union); e != nil {
		t.Fatal(e)
	}
	if open != "open-one" || useUnion && (subject != "union-one" || union != "union-one") || !useUnion && (subject != "open-one" || union != "") {
		t.Fatal("identifiers did not survive the encrypted completion flow", subject, open, union)
	}
	if _, e = s.CompleteQQ(ctx, f.Cookie, "123456", nil); e == nil {
		t.Fatal("flow replay accepted")
	}
}
