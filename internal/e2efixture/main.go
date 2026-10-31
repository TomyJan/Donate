//go:build e2e

// A separate test-only upstream and offline fixture writer. This package is never linked into Donate.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type grant struct{ Nonce, Challenge string }
type fixture struct {
	key         *rsa.PrivateKey
	issuer, app string
	mu          sync.Mutex
	codes       map[string]grant
	tokens      map[string]string
	orders      map[string]map[string]string
}

func canonical(v map[string]string) string {
	keys := []string{}
	for k, value := range v {
		if k != "sign" && k != "sign_type" && value != "" {
			keys = append(keys, k)
		}
	}
	slicesSort(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+v[k])
	}
	return strings.Join(parts, "&")
}
func slicesSort(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
func (f *fixture) sign(v map[string]string) {
	v["sign_type"] = "RSA"
	v["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	hash := sha256.Sum256([]byte(canonical(v)))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, 5, hash[:])
	v["sign"] = base64.StdEncoding.EncodeToString(sig)
}
func (f *fixture) jwt(g grant) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	h := enc(map[string]any{"alg": "RS256", "kid": "fixture", "typ": "JWT"})
	p := enc(map[string]any{"iss": f.issuer, "sub": "admin", "aud": "donate-test", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": g.Nonce, "name": "Fixture Admin", "picture": "https://public-cdn.tomys.top/head.png"})
	hash := sha256.Sum256([]byte(h + "." + p))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, 5, hash[:])
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (f *fixture) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/analytics/script.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = io.WriteString(w, `const script = document.currentScript;
fetch(new URL('/analytics/collect', script.src), {method: 'POST', mode: 'no-cors', body: script.dataset.websiteId}).then(() => { document.documentElement.dataset.umamiCollected = 'true'; });`)
	case "/analytics/collect":
		w.WriteHeader(http.StatusNoContent)
	case "/.well-known/openid-configuration":
		write(w, map[string]any{"issuer": f.issuer, "authorization_endpoint": f.issuer + "/authorize", "token_endpoint": f.issuer + "/token", "jwks_uri": f.issuer + "/jwks", "userinfo_endpoint": f.issuer + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}})
	case "/jwks":
		write(w, map[string]any{"keys": []any{map[string]any{"kty": "RSA", "use": "sig", "kid": "fixture", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("redirect_uri") != f.app+"/api/v1/user/oauth/callback/fixture-login" || q.Get("client_id") != "donate-test" || q.Get("code_challenge_method") != "S256" {
			http.Error(w, "invalid authorization", 400)
			return
		}
		code := auth.RandomToken()
		f.mu.Lock()
		f.codes[code] = grant{q.Get("nonce"), q.Get("code_challenge")}
		f.mu.Unlock()
		u, _ := url.Parse(q.Get("redirect_uri"))
		v := u.Query()
		v.Set("state", q.Get("state"))
		v.Set("code", code)
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), 302)
	case "/token":
		_ = r.ParseForm()
		f.mu.Lock()
		g, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		f.mu.Unlock()
		hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(hash[:]) != g.Challenge {
			http.Error(w, "invalid grant", 400)
			return
		}
		write(w, map[string]any{"access_token": "fixture-access", "token_type": "Bearer", "expires_in": 3600, "id_token": f.jwt(g)})
	case "/userinfo":
		write(w, map[string]any{"sub": "admin", "name": "Fixture Admin"})
	case "/api.php":
		action := r.URL.Query().Get("s")
		if r.Method != http.MethodPost || action != "pay/create" && action != "pay/query" && action != "pay/close" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		values := map[string]string{}
		for k := range r.PostForm {
			values[k] = r.PostForm.Get(k)
		}
		sig, e := base64.StdEncoding.DecodeString(values["sign"])
		hash := sha256.Sum256([]byte(canonical(values)))
		if e != nil || rsa.VerifyPKCS1v15(&f.key.PublicKey, 5, hash[:], sig) != nil {
			http.Error(w, "invalid signature", 400)
			return
		}
		no := values["out_trade_no"]
		f.mu.Lock()
		defer f.mu.Unlock()
		v := f.orders[no]
		if action == "pay/create" {
			if v == nil {
				v = map[string]string{"pid": "1001", "out_trade_no": no, "trade_no": "fixture-" + no, "money": values["money"], "type": values["type"], "status": "0"}
				f.orders[no] = v
			}
			out := map[string]string{"code": "0", "trade_no": v["trade_no"], "pay_type": "jump", "pay_info": f.issuer + "/checkout?order=" + no}
			f.sign(out)
			write(w, out)
			return
		}
		if v == nil {
			write(w, map[string]string{"code": "-1"})
			return
		}
		out := map[string]string{"code": "0"}
		if action == "pay/query" {
			for k, value := range v {
				out[k] = value
			}
		}
		f.sign(out)
		write(w, out)
	case "/checkout":
		no := r.URL.Query().Get("order")
		f.mu.Lock()
		v := f.orders[no]
		if v == nil {
			f.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		v["status"] = "1"
		out := map[string]string{"pid": "1001", "out_trade_no": no, "trade_no": v["trade_no"], "money": v["money"], "type": v["type"], "trade_status": "TRADE_SUCCESS"}
		f.sign(out)
		f.mu.Unlock()
		q := url.Values{}
		for k, value := range out {
			q.Set(k, value)
		}
		res, e := http.Get(f.app + "/api/v1/payment/notify?" + q.Encode())
		if e != nil {
			http.Error(w, "notify failed", 500)
			return
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(body) != "success" {
			http.Error(w, "notify rejected", 500)
			return
		}
		http.Redirect(w, r, f.app+"/?payment=returned", 303)
	default:
		http.NotFound(w, r)
	}
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	dir := os.Getenv("FIXTURE_DIR")
	if dir == "" {
		return fmt.Errorf("FIXTURE_DIR required")
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return e
	}
	issuer := "http://" + listener.Addr().String()
	app := os.Getenv("FIXTURE_APP_URL")
	cfg := config.Config{Development: true, PublicURL: app, Secret: make([]byte, 32), AdminIDs: map[string]bool{}}
	db, e := storage.Open(context.Background(), filepath.Join(dir, "donate.db"))
	if e != nil {
		return e
	}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, cfg, l)
	creds, e := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: issuer, Subject: "admin", Nickname: "Fixture Admin"})
	if e != nil {
		return e
	}
	other, e := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: issuer, Subject: "other", Nickname: "Other Donor"})
	if e != nil {
		return e
	}
	o := order.New(db, l, issuer, "1001", "v2")
	for i := range 13 {
		uid := creds.Session.UserID
		if i%2 == 0 {
			uid = other.Session.UserID
		}
		at := time.Now().Add(-time.Duration(15-i) * time.Hour)
		o.Now = func() time.Time { return at }
		v, e := o.Create(context.Background(), order.CreateInput{UserID: uid, AmountFen: int64(888 + i*100), Channel: "alipay", Message: fmt.Sprintf("测试留言 %d", i), IP: "192.0.2.1", Key: fmt.Sprint(i)})
		if e != nil {
			return e
		}
		_, e = o.Apply(context.Background(), order.Observation{MerchantOrderNo: v.MerchantOrderNo, MerchantID: "1001", GatewayID: issuer, TradeNo: fmt.Sprintf("seed-%d", i), Channel: "alipay", State: "paid", AmountFen: v.AmountFen, Verified: true, Source: "fixture"})
		if e != nil {
			return e
		}
		// Synthetic rendering fixtures for the reserved test IP, never real IP lookup data.
		province := "北京"
		if i%2 == 0 {
			province = "上海"
		}
		oid, _ := storage.ParseID(v.ID)
		lid := storage.NewID()
		if _, e = db.DB.Exec("INSERT INTO order_locations(id,order_id,country,province,source,source_version,queried_at) VALUES(?,?,'中国',?,'fixture-synthetic','e2e-reserved-ip',?)", lid[:], oid, province, at.Unix()); e != nil {
			return e
		}
	}
	if e = seedTaskQueue(db); e != nil {
		return e
	}
	db.Close()
	private := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	public, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})
	privatePath := filepath.Join(dir, "merchant.pem")
	publicPath := filepath.Join(dir, "platform.pem")
	if e = os.WriteFile(privatePath, private, 0600); e != nil {
		return e
	}
	if e = os.WriteFile(publicPath, publicPEM, 0600); e != nil {
		return e
	}
	env := map[string]string{"APP_ENV": "development", "PUBLIC_URL": app, "DATA_DIR": dir, "GEO_MODE": "disabled", "OIDC_ISSUER": issuer, "OIDC_CLIENT_ID": "donate-test", "ADMIN_USER_IDS": creds.Session.UserID, "EPAY_URL": issuer, "EPAY_PID": "1001", "EPAY_PROTOCOL": "v2", "EPAY_PRIVATE_KEY_FILE": privatePath, "EPAY_PUBLIC_KEY_FILE": publicPath}
	// Offline synthetic MMDB enables real geo task retries without external lookup.
	geoPath, e := filepath.Abs("internal/geo/testdata/synthetic-city.mmdb")
	if e != nil {
		return e
	}
	env["GEO_MODE"] = "local"
	env["GEO_DATABASE"] = geoPath
	env["OIDC_ID"] = "fixture-login"
	env["OIDC_NAME"] = "OIDC"
	env["UMAMI_SCRIPT_URL"] = issuer + "/analytics/script.js"
	env["UMAMI_WEBSITE_ID"] = "a4dca193-c108-47d7-a429-b89c13c7f747"
	b, _ := json.Marshal(env)
	if e = os.WriteFile(filepath.Join(dir, "environment.json"), b, 0600); e != nil {
		return e
	}
	f := &fixture{key: key, issuer: issuer, app: app, codes: map[string]grant{}, tokens: map[string]string{}, orders: map[string]map[string]string{}}
	fmt.Println("fixture ready", issuer)
	return http.Serve(listener, http.HandlerFunc(f.handler))
}
