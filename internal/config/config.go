package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type Config struct {
	LogOutputs                                                             []string
	AppName, LogDir, BackupDir                                             string
	LogRetentionDays, BackupRetentionDays, GeoUpdateDays                   int
	GeoXDBv4URL, GeoXDBv6URL, GeoMMDBURL                                   string
	Development                                                            bool
	Listen, PublicURL, DataDir, LogLevel                                   string
	Secret                                                                 []byte
	AdminIDs                                                               map[string]bool
	RealIPHeader                                                           string
	UmamiScriptURL, UmamiWebsiteID, UmamiOrigin                            string
	OIDCissuer, OIDCclientID, OIDCsecret                                   string
	OIDCid, OIDCname, OIDCiconURL                                          string
	QQappID, QQsecret                                                      string
	QQuseUnionID                                                           bool
	EpayURL, EpayPID, EpayProtocol, EpayKey, EpayPrivateKey, EpayPublicKey string
	PaymentRedirectHosts                                                   []string
	GeoMode, GeoPath                                                       string
	GeoXDBv4, GeoXDBv6                                                     string
	GeoOnlineFallback                                                      bool
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func Load() (Config, error) {
	c, err := load()
	if err == nil {
		return c, nil
	}
	if exampleErr := writeExample(); exampleErr != nil {
		return c, fmt.Errorf("%w；无法写出 .env.example，请检查当前目录权限", err)
	}
	return c, fmt.Errorf("%w；已写出 .env.example，请参考补齐或修正 .env 后重新启动", err)
}

func load() (Config, error) {
	data, err := os.ReadFile(".env")
	var values map[string]string
	if err == nil {
		values, err = parseEnv(data)
	}
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, errors.New("当前目录缺少 .env")
	}
	if err != nil {
		return Config{}, errors.New("读取 .env 失败，请检查文件格式与权限")
	}
	var missing []string
	for _, s := range settings {
		if _, exists := values[s.key]; !exists {
			missing = append(missing, s.key)
		}
	}
	if len(missing) != 0 {
		return Config{}, fmt.Errorf(".env 缺少配置键: %s（可选项也应声明空值）", strings.Join(missing, ", "))
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return Config{}, errors.New("加载 .env 失败，请检查配置变量")
			}
		}
	}
	c := Config{Development: os.Getenv("APP_ENV") == "development", Listen: env("LISTEN_ADDR", "127.0.0.1:8080"), PublicURL: strings.TrimRight(env("PUBLIC_URL", "https://donate.tomys.top"), "/"), DataDir: env("DATA_DIR", "data"), LogLevel: env("LOG_LEVEL", "INFO"), AdminIDs: map[string]bool{}, OIDCissuer: os.Getenv("OIDC_ISSUER"), OIDCclientID: os.Getenv("OIDC_CLIENT_ID"), OIDCsecret: os.Getenv("OIDC_CLIENT_SECRET"), QQappID: os.Getenv("QQ_APP_ID"), QQsecret: os.Getenv("QQ_APP_SECRET"), EpayURL: strings.TrimRight(os.Getenv("EPAY_URL"), "/"), EpayPID: os.Getenv("EPAY_PID"), EpayProtocol: env("EPAY_PROTOCOL", "v2"), EpayKey: os.Getenv("EPAY_KEY"), GeoMode: env("GEO_MODE", "online"), GeoPath: os.Getenv("GEO_DATABASE")}
	if err := c.loadRuntime(); err != nil {
		return c, err
	}
	c.UmamiScriptURL = strings.TrimSpace(os.Getenv("UMAMI_SCRIPT_URL"))
	c.UmamiWebsiteID = strings.TrimSpace(os.Getenv("UMAMI_WEBSITE_ID"))
	if c.UmamiScriptURL != "" || c.UmamiWebsiteID != "" {
		if c.UmamiScriptURL == "" || c.UmamiWebsiteID == "" {
			return c, errors.New("UMAMI_SCRIPT_URL 和 UMAMI_WEBSITE_ID 必须同时填写或同时留空")
		}
		tracker, e := url.Parse(c.UmamiScriptURL)
		if e != nil || tracker.Hostname() == "" || tracker.User != nil || tracker.Fragment != "" || !regexp.MustCompile(`^[A-Za-z0-9.:[\]_-]+$`).MatchString(tracker.Host) || (tracker.Scheme != "https" && !(c.Development && tracker.Scheme == "http")) {
			return c, errors.New("UMAMI_SCRIPT_URL 必须为不含凭据、片段及通配符的 HTTPS 脚本地址")
		}
		id, e := uuid.Parse(c.UmamiWebsiteID)
		if e != nil || id == uuid.Nil {
			return c, errors.New("UMAMI_WEBSITE_ID 必须为有效的网站 ID")
		}
		c.UmamiScriptURL, c.UmamiWebsiteID = tracker.String(), id.String()
		c.UmamiOrigin = tracker.Scheme + "://" + tracker.Host
	}
	if _, err := c.PublicOrigin(); err != nil {
		return c, err
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
	c.GeoXDBv4 = strings.TrimSpace(os.Getenv("GEO_XDB_IPV4"))
	c.GeoXDBv6 = strings.TrimSpace(os.Getenv("GEO_XDB_IPV6"))
	c.GeoPath = strings.TrimSpace(c.GeoPath)
	paths := map[string]string{}
	for _, item := range []struct{ key, path, url string }{
		{"GEO_XDB_IPV4", c.GeoXDBv4, c.GeoXDBv4URL},
		{"GEO_XDB_IPV6", c.GeoXDBv6, c.GeoXDBv6URL},
		{"GEO_DATABASE", c.GeoPath, c.GeoMMDBURL},
	} {
		if item.path == "" {
			if item.url != "" {
				return c, fmt.Errorf("配置下载地址时 %s 不能留空", item.key)
			}
			continue
		}
		path, err := filepath.Abs(item.path)
		if err != nil {
			return c, fmt.Errorf("%s 路径无效", item.key)
		}
		for previous, key := range paths {
			if path == previous || runtime.GOOS == "windows" && strings.EqualFold(path, previous) {
				return c, fmt.Errorf("%s 与 %s 的库路径不能相同", item.key, key)
			}
		}
		paths[path] = item.key
	}
	if c.GeoMode == "local" && c.GeoPath == "" && c.GeoXDBv4 == "" && c.GeoXDBv6 == "" {
		return c, errors.New("本地属地模式需要 GEO_XDB_IPV4、GEO_XDB_IPV6 或 GEO_DATABASE")
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	return c, err
}

func (c *Config) loadRuntime() error {
	c.AppName = env("APP_NAME", "donate")
	if len(c.AppName) > 63 || !regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`).MatchString(c.AppName) {
		return errors.New("APP_NAME 只允许小写英文字母与 -，须以字母开头结尾，最多 63 字符")
	}
	if v := env("APP_ENV", "production"); v != "production" && v != "development" {
		return errors.New("APP_ENV 仅支持 production / development")
	}
	_, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return errors.New("LISTEN_ADDR 必须为有效的 host:port 监听地址")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("LISTEN_ADDR 的端口须为 1 至 65535")
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return errors.New("LOG_LEVEL 无效")
	}
	selected := map[string]bool{}
	for _, output := range strings.Split(os.Getenv("LOG_OUTPUTS"), ",") {
		output = strings.TrimSpace(output)
		if output != "console" && output != "log" && output != "jsonl" {
			return errors.New("LOG_OUTPUTS 至少选择一项 console、log、jsonl，以逗号分隔")
		}
		if !selected[output] {
			c.LogOutputs = append(c.LogOutputs, output)
			selected[output] = true
		}
	}
	c.LogDir, err = filepath.Abs(env("LOG_DIR", "data/logs"))
	if err != nil {
		return errors.New("LOG_DIR 路径无效")
	}
	c.BackupDir, err = filepath.Abs(env("BACKUP_DIR", "data/backups"))
	if err != nil {
		return errors.New("BACKUP_DIR 路径无效")
	}
	for _, item := range []struct {
		key, fallback string
		target        *int
	}{
		{"LOG_RETENTION_DAYS", "30", &c.LogRetentionDays},
		{"BACKUP_RETENTION_DAYS", "7", &c.BackupRetentionDays},
		{"GEO_UPDATE_DAYS", "7", &c.GeoUpdateDays},
	} {
		v, err := strconv.Atoi(env(item.key, item.fallback))
		if err != nil || v < 0 || v > 36500 {
			return fmt.Errorf("%s 必须为 0 至 36500 的整数天数", item.key)
		}
		*item.target = v
	}
	if c.GeoOnlineFallback, err = strconv.ParseBool(env("GEO_ONLINE_FALLBACK", "false")); err != nil {
		return errors.New("GEO_ONLINE_FALLBACK 必须为布尔值")
	}
	for _, item := range []struct {
		key    string
		target *string
	}{
		{"GEO_XDB_IPV4_URL", &c.GeoXDBv4URL}, {"GEO_XDB_IPV6_URL", &c.GeoXDBv6URL}, {"GEO_DATABASE_URL", &c.GeoMMDBURL},
	} {
		v := strings.TrimSpace(os.Getenv(item.key))
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err != nil || u.Hostname() == "" || u.Fragment != "" || (u.Scheme != "https" && !(c.Development && u.Scheme == "http")) {
			return fmt.Errorf("%s 必须为有效 HTTPS 下载地址", item.key)
		}
		*item.target = v
	}
	return nil
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
