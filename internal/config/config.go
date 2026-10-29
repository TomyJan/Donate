package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Config struct {
	Development                                                            bool
	Listen, PublicURL, DataDir, LogLevel                                   string
	Secret                                                                 []byte
	AdminIDs                                                               map[string]bool
	TrustedProxies                                                         []netip.Prefix
	OIDCissuer, OIDCclientID, OIDCsecret                                   string
	QQappID, QQsecret                                                      string
	EpayURL, EpayPID, EpayProtocol, EpayKey, EpayPrivateKey, EpayPublicKey string
	PaymentRedirectHosts                                                   []string
	GeoMode, GeoPath                                                       string
	GeoOnlineFallback                                                      bool
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func Load() (Config, error) {
	c := Config{Development: os.Getenv("APP_ENV") == "development", Listen: env("LISTEN_ADDR", "127.0.0.1:8080"), PublicURL: strings.TrimRight(env("PUBLIC_URL", "https://donate.tomys.top"), "/"), DataDir: env("DATA_DIR", "data"), LogLevel: env("LOG_LEVEL", "INFO"), AdminIDs: map[string]bool{}, OIDCissuer: os.Getenv("OIDC_ISSUER"), OIDCclientID: os.Getenv("OIDC_CLIENT_ID"), OIDCsecret: os.Getenv("OIDC_CLIENT_SECRET"), QQappID: os.Getenv("QQ_APP_ID"), QQsecret: os.Getenv("QQ_APP_SECRET"), EpayURL: strings.TrimRight(os.Getenv("EPAY_URL"), "/"), EpayPID: os.Getenv("EPAY_PID"), EpayProtocol: env("EPAY_PROTOCOL", "v2"), EpayKey: os.Getenv("EPAY_KEY"), GeoMode: env("GEO_MODE", "online"), GeoPath: os.Getenv("GEO_DATABASE"), GeoOnlineFallback: os.Getenv("GEO_ONLINE_FALLBACK") == "true"}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(c.Development && u.Scheme == "http")) {
		return c, errors.New("PUBLIC_URL 必须为无路径、查询或凭据的 HTTPS 站点地址")
	}
	for _, s := range strings.Split(os.Getenv("ADMIN_USER_IDS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			id, e := uuid.Parse(s)
			if e != nil {
				return c, errors.New("ADMIN_USER_IDS 包含无效 ID")
			}
			c.AdminIDs[id.String()] = true
		}
	}
	for _, s := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			p, e := netip.ParsePrefix(s)
			if e != nil {
				return c, fmt.Errorf("TRUSTED_PROXY_CIDRS 无效: %w", e)
			}
			c.TrustedProxies = append(c.TrustedProxies, p)
		}
	}
	if s := os.Getenv("APP_SECRET"); s != "" {
		c.Secret, err = base64.StdEncoding.DecodeString(s)
		if err != nil || len(c.Secret) != 32 {
			return c, errors.New("APP_SECRET 必须是 Base64 编码的 32 字节随机密钥")
		}
	} else if c.Development {
		c.Secret = make([]byte, 32)
		if _, err = rand.Read(c.Secret); err != nil {
			return c, err
		}
	} else {
		return c, errors.New("缺少 APP_SECRET")
	}
	if (c.OIDCissuer != "" || c.OIDCclientID != "" || c.OIDCsecret != "") && (c.OIDCissuer == "" || c.OIDCclientID == "") {
		return c, errors.New("OIDC 配置不完整")
	}
	if (c.QQappID == "") != (c.QQsecret == "") {
		return c, errors.New("QQ 配置不完整")
	}
	if c.EpayProtocol != "v1" && c.EpayProtocol != "v2" {
		return c, errors.New("EPAY_PROTOCOL 仅支持 v1 / v2")
	}
	if c.EpayPrivateKey, err = readSecret("EPAY_PRIVATE_KEY"); err != nil {
		return c, err
	}
	if c.EpayPublicKey, err = readSecret("EPAY_PUBLIC_KEY"); err != nil {
		return c, err
	}
	if c.EpayURL != "" {
		p, e := url.Parse(c.EpayURL)
		if e != nil || p.Host == "" || p.User != nil || p.RawQuery != "" || p.Fragment != "" || (p.Scheme != "https" && !c.Development) {
			return c, errors.New("EPAY_URL 必须是可信 HTTPS 地址")
		}
		c.PaymentRedirectHosts = append(c.PaymentRedirectHosts, strings.ToLower(p.Hostname()))
		if c.EpayPID == "" || (c.EpayProtocol == "v1" && c.EpayKey == "") || (c.EpayProtocol == "v2" && (c.EpayPrivateKey == "" || c.EpayPublicKey == "")) {
			return c, errors.New("易支付凭据不完整")
		}
	}
	for _, s := range strings.Split(os.Getenv("EPAY_REDIRECT_HOSTS"), ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			if strings.ContainsAny(s, "/:@* ") {
				return c, errors.New("EPAY_REDIRECT_HOSTS 必须是明确域名")
			}
			c.PaymentRedirectHosts = append(c.PaymentRedirectHosts, s)
		}
	}
	if c.GeoMode != "online" && c.GeoMode != "local" && c.GeoMode != "disabled" {
		return c, errors.New("GEO_MODE 无效")
	}
	if c.GeoMode == "local" && c.GeoPath == "" {
		return c, errors.New("本地属地模式需要 GEO_DATABASE")
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	return c, err
}

func readSecret(name string) (string, error) {
	if p := os.Getenv(name + "_FILE"); p != "" {
		b, e := os.ReadFile(p)
		if e != nil {
			return "", fmt.Errorf("读取 %s_FILE 失败", name)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(os.Getenv(name)), nil
}
