package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type Config struct {
	Development                                                            bool
	Listen, PublicURL, DataDir, LogLevel                                   string
	Secret                                                                 []byte
	AdminIDs                                                               map[string]bool
	RealIPHeader                                                           string
	OIDCissuer, OIDCclientID, OIDCsecret                                   string
	OIDCid, OIDCname, OIDCiconURL                                          string
	QQappID, QQsecret                                                      string
	QQuseUnionID                                                           bool
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
	data, err := os.ReadFile(".env")
	var values map[string]string
	if err == nil {
		values, err = parseEnv(data)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, errors.New("读取 .env 失败，请检查文件格式与权限")
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return Config{}, errors.New("加载 .env 失败，请检查配置变量")
			}
		}
	}
	c := Config{Development: os.Getenv("APP_ENV") == "development", Listen: env("LISTEN_ADDR", "127.0.0.1:8080"), PublicURL: strings.TrimRight(env("PUBLIC_URL", "https://donate.tomys.top"), "/"), DataDir: env("DATA_DIR", "."), LogLevel: env("LOG_LEVEL", "INFO"), AdminIDs: map[string]bool{}, OIDCissuer: os.Getenv("OIDC_ISSUER"), OIDCclientID: os.Getenv("OIDC_CLIENT_ID"), OIDCsecret: os.Getenv("OIDC_CLIENT_SECRET"), QQappID: os.Getenv("QQ_APP_ID"), QQsecret: os.Getenv("QQ_APP_SECRET"), EpayURL: strings.TrimRight(os.Getenv("EPAY_URL"), "/"), EpayPID: os.Getenv("EPAY_PID"), EpayProtocol: env("EPAY_PROTOCOL", "v2"), EpayKey: os.Getenv("EPAY_KEY"), GeoMode: env("GEO_MODE", "online"), GeoPath: os.Getenv("GEO_DATABASE"), GeoOnlineFallback: os.Getenv("GEO_ONLINE_FALLBACK") == "true"}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(c.Development && u.Scheme == "http")) {
		return c, errors.New("PUBLIC_URL 必须为无路径、查询或凭据的 HTTPS 站点地址")
	}
	c.OIDCid, c.OIDCname, c.OIDCiconURL = env("OIDC_ID", "oidc"), env("OIDC_NAME", "OIDC"), os.Getenv("OIDC_ICON_URL")
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`).MatchString(c.OIDCid) || c.OIDCid == "qq" {
		return c, errors.New("OIDC_ID 必须为 1 至 32 位小写字母、数字、下划线或连字符，以字母开头且不能为 qq")
	}
	c.OIDCname = strings.TrimSpace(c.OIDCname)
	if c.OIDCname == "" || len([]rune(c.OIDCname)) > 64 || strings.ContainsAny(c.OIDCname, "\r\n\t") {
		return c, errors.New("OIDC_NAME 必须为 1 至 64 字的单行名称")
	}
	if c.OIDCiconURL != "" {
		icon, e := url.Parse(c.OIDCiconURL)
		if e != nil || icon.Scheme != "https" || icon.Hostname() == "" || icon.User != nil || icon.Fragment != "" {
			return c, errors.New("OIDC_ICON_URL 必须为不含凭据及片段的 HTTPS 图片地址")
		}
	}
	if c.QQuseUnionID, err = strconv.ParseBool(env("QQ_USE_UNIONID", "false")); err != nil {
		return c, errors.New("QQ_USE_UNIONID 必须为布尔值")
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
	c.RealIPHeader = os.Getenv("REAL_IP_HEADER")
	if c.RealIPHeader != "" {
		if !regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$").MatchString(c.RealIPHeader) {
			return c, errors.New("REAL_IP_HEADER 必须为单个有效的 HTTP 请求头名称")
		}
		c.RealIPHeader = http.CanonicalHeaderKey(c.RealIPHeader)
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

// godotenv treats an empty unquoted assignment's trailing comment as its value.
// Normalize whitespace-separated comments only, preserving literal hashes,
// quoted/multiline content and the library's KEY: value syntax.
func parseEnv(data []byte) (map[string]string, error) {
	var normalized strings.Builder
	var quote byte
	for _, line := range strings.SplitAfter(string(data), "\n") {
		value := line
		if quote == 0 {
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
				normalized.WriteString(line)
				continue
			}
			separator := strings.IndexAny(line, "=:")
			if separator < 0 {
				normalized.WriteString(line)
				continue
			}
			tail := line[separator+1:]
			value = strings.TrimLeft(tail, " \t")
			if len(value) < len(tail) && strings.HasPrefix(value, "#") {
				normalized.WriteString(line[:len(line)-len(value)])
				normalized.WriteString("\"\" " + value)
				continue
			}
			if len(value) == 0 || value[0] != '\'' && value[0] != '"' {
				normalized.WriteString(line)
				continue
			}
			quote, value = value[0], value[1:]
		}
		for i := range len(value) {
			if value[i] == quote && (i == 0 || value[i-1] != '\\') {
				quote = 0
				break
			}
		}
		normalized.WriteString(line)
	}
	return godotenv.Unmarshal(normalized.String())
}

// The public ID is a routing identifier. Stored OIDC identity keys remain issuer + subject.
func (c Config) OIDCProviderID() string {
	if c.OIDCid == "" {
		return "oidc"
	}
	return c.OIDCid
}

type LoginProvider struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	IconURL string `json:"icon_url"`
}

func (c Config) LoginProviders() []LoginProvider {
	providers := []LoginProvider{}
	if c.OIDCissuer != "" {
		name := c.OIDCname
		if name == "" {
			name = "OIDC"
		}
		providers = append(providers, LoginProvider{ID: c.OIDCProviderID(), Kind: "oidc", Name: name, IconURL: c.OIDCiconURL})
	}
	if c.QQappID != "" {
		providers = append(providers, LoginProvider{ID: "qq", Kind: "qq", Name: "QQ"})
	}
	return providers
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
