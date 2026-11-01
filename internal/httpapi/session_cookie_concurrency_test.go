package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

// These are independent acceptance tests. Every login goes through the real
// Handler; only the provider network is simulated. No product internals are
// used to choose a cookie or a browser family.
func TestAnonymousConcurrentLoginNeverRestoresOlderIdentity(t *testing.T) {
	for _, processing := range []bool{false, true} {
		for _, delivery := range []bool{false, true} {
			for _, invalidation := range []string{"logout", "banned", "expired", "missing-slot"} {
				t.Run(fmt.Sprintf("processingReverse=%v/deliveryReverse=%v/%s", processing, delivery, invalidation), func(t *testing.T) {
					s, h, a := setup(t)
					s.Config.Development, a.Config.Development = false, false
					s.Config.PublicURL, a.Config.PublicURL = "https://donate.example", "https://donate.example"
					a.Config.QQappID, a.Config.QQsecret = "123", "secret"
					ctx := context.Background()
					want := make(map[string]string)
					for i, subject := range []string{"b", "c"} {
						creds, err := a.LoginIdentity(ctx, auth.Identity{Provider: "qq", Issuer: "123", Subject: subject, QQNumber: fmt.Sprint(12345 + i)})
						if err != nil {
							t.Fatal(err)
						}
						want[subject] = creds.Session.UserID
					}
					a.HTTP = &http.Client{Transport: reauthenticationTransport(func(r *http.Request) (*http.Response, error) {
						var body string
						switch r.URL.Path {
						case "/oauth2.0/token":
							body = fmt.Sprintf("{\"access_token\":%q}", r.URL.Query().Get("code"))
						case "/oauth2.0/me":
							body = fmt.Sprintf("{\"client_id\":\"123\",\"openid\":%q}", r.URL.Query().Get("access_token"))
						case "/user/get_user_info":
							body = "{\"ret\":0,\"nickname\":\"Concurrent\",\"figureurl_qq_2\":\"https://qlogo.cn/known\"}"
						default:
							return nil, fmt.Errorf("unexpected provider request: %s", r.URL)
						}
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})}
					origin, _ := url.Parse(s.Config.PublicURL)
					jar, _ := cookiejar.New(nil)
					makeReq := func(method, path, body, csrf string) *http.Request {
						r := httptest.NewRequest(method, path, strings.NewReader(body))
						if method == "POST" {
							r.Header.Set("Origin", s.Config.PublicURL)
							r.Header.Set("X-CSRF-Token", csrf)
						}
						for _, c := range jar.Cookies(origin) {
							r.AddCookie(c)
						}
						return r
					}
					send := func(r *http.Request) *httptest.ResponseRecorder {
						w := httptest.NewRecorder()
						h.ServeHTTP(w, r)
						return w
					}
					decode := func(w *httptest.ResponseRecorder) map[string]any {
						t.Helper()
						var out struct {
							Code string
							Data map[string]any
						}
						if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Code != "OK" {
							t.Fatalf("response %d %s: %v", w.Code, w.Body.String(), err)
						}
						return out.Data
					}
					// Independent first-page requests have unrelated anonymous CSRF nonces.
					sites := []*httptest.ResponseRecorder{
						send(makeReq("GET", "/api/v1/site/info", "", "")),
						send(makeReq("GET", "/api/v1/site/info", "", "")),
					}
					reqs := make([]*http.Request, 2)
					for i, site := range sites {
						csrf := decode(site)["csrf_token"].(string)
						jar.SetCookies(origin, site.Result().Cookies())
						reqs[i] = makeReq("POST", "/api/v1/user/login", "{\"provider\":\"qq\",\"return_to\":\"/\"}", csrf)
					}
					// Both real POST requests leave before either begin response arrives.
					begins := []*httptest.ResponseRecorder{send(reqs[0]), send(reqs[1])}
					callbacks := make([]*http.Request, 2)
					for i, b := range begins {
						u, err := url.Parse(decode(b)["authorization_url"].(string))
						if err != nil {
							t.Fatal(err)
						}
						jar.SetCookies(origin, b.Result().Cookies())
						// Each callback request is sent while its own fixed flow cookie
						// is current; their responses and completion can still cross.
						callbacks[i] = makeReq("GET", "/api/v1/user/oauth/callback/qq?state="+url.QueryEscape(u.Query().Get("state"))+"&code="+[]string{"b", "c"}[i], "", "")
					}
					order := []int{0, 1}
					if processing {
						order = []int{1, 0}
					}
					responses := make([]*httptest.ResponseRecorder, 2)
					for _, i := range order {
						responses[i] = send(callbacks[i])
						if responses[i].Code != 303 || responses[i].Header().Get("Location") != "/" {
							t.Fatalf("callback %d: %d %s %s", i, responses[i].Code, responses[i].Header().Get("Location"), responses[i].Body.String())
						}
					}
					arrival := []int{0, 1}
					if delivery {
						arrival = []int{1, 0}
					}
					for _, i := range arrival {
						jar.SetCookies(origin, responses[i].Result().Cookies())
					}
					latest := []string{"b", "c"}[order[1]]
					profile := send(makeReq("GET", "/api/v1/user/profile", "", ""))
					data := decode(profile)
					if data["id"] != want[latest] {
						t.Fatalf("response delivery changed current identity: got %v want %s", data["id"], want[latest])
					}
					currentCSRF := data["csrf_token"].(string)
					currentCookie := ""
					for _, c := range jar.Cookies(origin) {
						if ss, err := a.Resolve(ctx, c.Value); err == nil && ss.UserID == want[latest] {
							currentCookie = c.Name
						}
					}
					if currentCookie == "" {
						t.Fatal("no presented current authentication token")
					}
					// A browser anchor is metadata, never an authentication credential.
					anchorReq := makeReq("GET", "/api/v1/user/profile", "", "")
					anchorReq.Header.Del("Cookie")
					for _, c := range jar.Cookies(origin) {
						if !strings.HasPrefix(c.Name, s.sessionName()) {
							anchorReq.AddCookie(c)
						}
					}
					if w := send(anchorReq); w.Code != 401 {
						t.Fatalf("anchor-only authenticated: %d %s", w.Code, w.Body.String())
					}
					switch invalidation {
					case "logout":
						w := send(makeReq("POST", "/api/v1/user/logout", "{}", currentCSRF))
						decode(w)
						jar.SetCookies(origin, w.Result().Cookies())
					case "banned":
						uid, _ := storage.ParseID(want[latest])
						if _, err := a.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", uid); err != nil {
							t.Fatal(err)
						}
					case "expired":
						uid, _ := storage.ParseID(want[latest])
						if _, err := a.Store.DB.Exec("UPDATE sessions SET expires_at=? WHERE user_id=?", a.Now().Add(-1).Unix(), uid); err != nil {
							t.Fatal(err)
						}
					case "missing-slot":
						jar.SetCookies(origin, []*http.Cookie{{Name: currentCookie, Path: "/", MaxAge: -1, Secure: true}})
					}
					// A genuinely older successful login response can arrive now.
					jar.SetCookies(origin, responses[order[0]].Result().Cookies())
					after := send(makeReq("GET", "/api/v1/user/profile", "", ""))
					if after.Code != 401 && after.Code != 403 {
						t.Fatalf("old response restored an identity after current %s: %d %s", invalidation, after.Code, after.Body.String())
					}
				})
			}
		}
	}
}
