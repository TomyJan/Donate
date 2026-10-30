package payment

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
)

var ErrUnavailable = errors.New("易支付暂不可用或返回未知结果")

type retryError struct{ wait time.Duration }

func (e *retryError) Error() string             { return ErrUnavailable.Error() }
func (e *retryError) Unwrap() error             { return ErrUnavailable }
func (e *retryError) RetryAfter() time.Duration { return e.wait }

func retryDelay(value string, now time.Time) time.Duration {
	var wait time.Duration
	if seconds, e := strconv.ParseInt(value, 10, 32); e == nil {
		wait = time.Duration(seconds) * time.Second
	} else if at, e := http.ParseTime(value); e == nil {
		wait = at.Sub(now)
	}
	return min(max(wait, 0), 24*time.Hour)
}

type Gateway struct {
	Config  config.Config
	HTTP    *http.Client
	Now     func() time.Time
	private *rsa.PrivateKey
	public  *rsa.PublicKey
}

func NewGateway(c config.Config) (*Gateway, error) {
	g := &Gateway{Config: c, Now: time.Now, HTTP: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	var e error
	if c.EpayPrivateKey != "" {
		g.private, e = privateKey(c.EpayPrivateKey)
		if e != nil {
			return nil, e
		}
	}
	if c.EpayPublicKey != "" {
		g.public, e = publicKey(c.EpayPublicKey)
		if e != nil {
			return nil, e
		}
	}
	return g, nil
}
func (g *Gateway) Ready() bool { return g.Config.EpayURL != "" && g.Config.EpayPID != "" }
func (g *Gateway) account(o order.Order) error {
	if !g.Ready() || o.GatewayID != g.Config.EpayURL || o.MerchantID != g.Config.EpayPID {
		return ErrUnavailable
	}
	if o.Protocol == "v1" && g.Config.EpayKey == "" || o.Protocol == "v2" && (g.private == nil || g.public == nil) {
		return ErrUnavailable
	}
	return nil
}
func (g *Gateway) call(ctx context.Context, path, protocol string, values map[string]string) (map[string]string, error) {
	values["pid"] = g.Config.EpayPID
	form := url.Values{}
	method := "POST"
	endpoint := g.Config.EpayURL + "/" + path
	if protocol == "v1" {
		values["key"] = g.Config.EpayKey
		method = "GET"
	} else {
		// This is the same V2 PHP handler used by /api/pay/*, without
		// requiring the gateway's web server to install rewrite rules.
		endpoint = g.Config.EpayURL + "/api.php?s=" + url.QueryEscape(strings.TrimPrefix(path, "api/"))
		if e := g.sign(values, protocol); e != nil {
			return nil, e
		}
	}
	for k, v := range values {
		form.Set(k, v)
	}
	var body io.Reader
	if method == "GET" {
		endpoint += "?" + form.Encode()
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, body)
	if e != nil {
		return nil, ErrProtocol
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	r, e := g.HTTP.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		if wait := retryDelay(r.Header.Get("Retry-After"), g.Now()); wait > 0 {
			return nil, &retryError{wait: wait}
		}
		return nil, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 64*1024+1))
	if e != nil || len(b) > 64*1024 {
		return nil, ErrProtocol
	}
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(b))
	if e = decoder.Decode(&raw); e != nil || raw == nil {
		return nil, ErrProtocol
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, ErrProtocol
	}
	out := map[string]string{}
	for k, v := range raw {
		var text string
		if json.Unmarshal(v, &text) == nil {
			out[k] = text
			continue
		}
		var number json.Number
		if json.Unmarshal(v, &number) == nil && number != "" {
			out[k] = number.String()
			continue
		}
		if bytes.Equal(v, []byte("null")) {
			continue
		}
		if len(v) > 0 && (v[0] == '[' || v[0] == '{') {
			continue
		}
		return nil, ErrProtocol
	}
	code := out["code"]
	if protocol == "v2" {
		if code != "0" {
			return nil, ErrUnavailable
		}
		if e = g.verify(out, protocol); e != nil {
			return nil, e
		}
	} else if code != "1" {
		return nil, ErrUnavailable
	}
	return out, nil
}

type Checkout struct {
	Mode   string            `json:"mode"`
	URL    string            `json:"url"`
	Fields map[string]string `json:"fields,omitempty"`
}

func (g *Gateway) Create(ctx context.Context, o order.Order) (Checkout, string, error) {
	if e := g.account(o); e != nil {
		return Checkout{}, "", e
	}
	v := map[string]string{"pid": g.Config.EpayPID, "type": o.Channel, "out_trade_no": o.MerchantOrderNo, "notify_url": g.Config.PublicURL + "/api/v1/payment/notify", "return_url": g.Config.PublicURL + "/api/v1/payment/return", "name": "捐赠 TomyJan", "money": Money(o.AmountFen)}
	if o.Protocol == "v1" {
		if e := g.sign(v, "v1"); e != nil {
			return Checkout{}, "", e
		}
		return Checkout{Mode: "form_post", URL: g.Config.EpayURL + "/submit.php", Fields: v}, "", nil
	}
	v["method"] = "jump"
	v["clientip"] = o.IP
	out, e := g.call(ctx, "api/pay/create", "v2", v)
	if e != nil {
		return Checkout{}, "", e
	}
	if out["trade_no"] == "" || out["pay_type"] != "jump" || !g.validRedirect(out["pay_info"]) {
		return Checkout{}, "", ErrProtocol
	}
	return Checkout{Mode: "redirect", URL: out["pay_info"]}, out["trade_no"], nil
}
func (g *Gateway) validRedirect(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && !(g.Config.Development && u.Scheme == "http")) {
		return false
	}
	if u.Port() != "" && u.Port() != "443" && !g.Config.Development {
		return false
	}
	for _, host := range g.Config.PaymentRedirectHosts {
		if strings.EqualFold(host, u.Hostname()) {
			return true
		}
	}
	return false
}
func (g *Gateway) Query(ctx context.Context, o order.Order) (order.Observation, error) {
	if e := g.account(o); e != nil {
		return order.Observation{}, e
	}
	path := "api/pay/query"
	v := map[string]string{"out_trade_no": o.MerchantOrderNo}
	if o.Protocol == "v1" {
		path = "api.php"
		v["act"] = "order"
	}
	out, e := g.call(ctx, path, o.Protocol, v)
	if e != nil {
		return order.Observation{}, e
	}
	return g.observation(o, out, false)
}
func (g *Gateway) Notification(o order.Order, v map[string]string) (order.Observation, error) {
	if e := g.account(o); e != nil {
		return order.Observation{}, e
	}
	if e := g.verify(v, o.Protocol); e != nil {
		return order.Observation{}, e
	}
	return g.observation(o, v, true)
}
func (g *Gateway) observation(o order.Order, v map[string]string, notify bool) (order.Observation, error) {
	money, e := ParseMoney(v["money"])
	if e != nil {
		return order.Observation{}, e
	}
	if v["out_trade_no"] != o.MerchantOrderNo || v["pid"] != o.MerchantID || v["trade_no"] == "" || v["type"] != "alipay" && v["type"] != "wxpay" {
		return order.Observation{}, ErrProtocol
	}
	state := "unknown"
	if notify {
		if v["trade_status"] != "TRADE_SUCCESS" {
			return order.Observation{}, ErrProtocol
		}
		state = "paid"
	} else {
		switch v["status"] {
		case "0":
			state = "unpaid"
		case "1":
			state = "paid"
		case "2":
			state = "refunded"
		case "3":
			state = "frozen"
		case "4":
			state = "authorized"
		default:
			return order.Observation{}, ErrProtocol
		}
	}
	var paid time.Time
	if v["endtime"] != "" {
		zone := time.FixedZone("Asia/Shanghai", 8*60*60)
		paid, e = time.ParseInLocation("2006-01-02 15:04:05", v["endtime"], zone)
		if e != nil {
			return order.Observation{}, ErrProtocol
		}
	}
	return order.Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: v["pid"], GatewayID: o.GatewayID, TradeNo: v["trade_no"], APITradeNo: v["api_trade_no"], Channel: v["type"], AmountFen: money, State: state, PaidAt: paid, Verified: true}, nil
}
func (g *Gateway) Close(ctx context.Context, o order.Order) error {
	if e := g.account(o); e != nil {
		return e
	}
	if o.Protocol != "v2" {
		return ErrUnavailable
	}
	_, e := g.call(ctx, "api/pay/close", "v2", map[string]string{"out_trade_no": o.MerchantOrderNo})
	return e
}
func Timestamp(v int64) string { return strconv.FormatInt(v, 10) }
