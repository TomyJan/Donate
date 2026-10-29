package payment

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

var ErrProtocol = errors.New("易支付协议校验失败")

func canonical(v map[string]string) string {
	keys := make([]string, 0, len(v))
	for k, x := range v {
		if k != "sign" && k != "sign_type" && strings.TrimSpace(x) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+v[k])
	}
	return strings.Join(parts, "&")
}
func ParseValues(v url.Values) (map[string]string, error) {
	out := map[string]string{}
	for k, x := range v {
		if len(x) != 1 || strings.ContainsAny(k, "[]") || len(k) > 128 || len(x[0]) > 8192 {
			return nil, ErrProtocol
		}
		out[k] = x[0]
	}
	return out, nil
}
func keyDER(raw, kind string) ([]byte, error) {
	if block, _ := pem.Decode([]byte(raw)); block != nil {
		return block.Bytes, nil
	}
	b, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
	if e != nil || len(b) == 0 {
		return nil, errors.New(kind + " 格式无效")
	}
	return b, nil
}
func privateKey(raw string) (*rsa.PrivateKey, error) {
	b, e := keyDER(raw, "商户私钥")
	if e != nil {
		return nil, e
	}
	v, e := x509.ParsePKCS8PrivateKey(b)
	var k *rsa.PrivateKey
	if e == nil {
		k, _ = v.(*rsa.PrivateKey)
	} else {
		k, e = x509.ParsePKCS1PrivateKey(b)
	}
	if e != nil || k == nil || k.N.BitLen() < 2048 {
		return nil, errors.New("商户 RSA 私钥无效或不足 2048 位")
	}
	return k, nil
}
func publicKey(raw string) (*rsa.PublicKey, error) {
	b, e := keyDER(raw, "平台公钥")
	if e != nil {
		return nil, e
	}
	v, e := x509.ParsePKIXPublicKey(b)
	var k *rsa.PublicKey
	if e == nil {
		k, _ = v.(*rsa.PublicKey)
	} else {
		k, e = x509.ParsePKCS1PublicKey(b)
	}
	if e != nil || k == nil || k.N.BitLen() < 2048 {
		return nil, errors.New("平台 RSA 公钥无效或不足 2048 位")
	}
	return k, nil
}
func (g *Gateway) sign(v map[string]string, protocol string) error {
	if protocol == "v1" {
		if g.Config.EpayKey == "" {
			return ErrProtocol
		}
		h := md5.Sum([]byte(canonical(v) + g.Config.EpayKey))
		v["sign"] = hex.EncodeToString(h[:])
		v["sign_type"] = "MD5"
		return nil
	}
	if protocol != "v2" || g.private == nil {
		return ErrProtocol
	}
	v["timestamp"] = strconv.FormatInt(g.Now().Unix(), 10)
	h := sha256.Sum256([]byte(canonical(v)))
	sig, e := rsa.SignPKCS1v15(rand.Reader, g.private, crypto.SHA256, h[:])
	if e != nil {
		return ErrProtocol
	}
	v["sign"] = base64.StdEncoding.EncodeToString(sig)
	v["sign_type"] = "RSA"
	return nil
}
func (g *Gateway) verify(v map[string]string, protocol string) error {
	if protocol == "v1" {
		if v["sign_type"] != "MD5" || g.Config.EpayKey == "" {
			return ErrProtocol
		}
		h := md5.Sum([]byte(canonical(v) + g.Config.EpayKey))
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(h[:])), []byte(v["sign"])) != 1 {
			return ErrProtocol
		}
		return nil
	}
	if protocol != "v2" || v["sign_type"] != "RSA" || g.public == nil {
		return ErrProtocol
	}
	at, e := strconv.ParseInt(v["timestamp"], 10, 64)
	if e != nil || at < g.Now().Unix()-300 || at > g.Now().Unix()+300 {
		return ErrProtocol
	}
	sig, e := base64.StdEncoding.Strict().DecodeString(v["sign"])
	if e != nil {
		return ErrProtocol
	}
	h := sha256.Sum256([]byte(canonical(v)))
	if rsa.VerifyPKCS1v15(g.public, crypto.SHA256, h[:], sig) != nil {
		return ErrProtocol
	}
	return nil
}
func Money(fen int64) string { return strconv.FormatInt(fen/100, 10) + "." + fmtCents(fen%100) }
func fmtCents(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}
func ParseMoney(v string) (int64, error) {
	parts := strings.Split(v, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 8 {
		return 0, ErrProtocol
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0, ErrProtocol
		}
	}
	whole, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return 0, ErrProtocol
	}
	cents := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, ErrProtocol
		}
		for _, r := range parts[1] {
			if r < '0' || r > '9' {
				return 0, ErrProtocol
			}
		}
		cents, e = strconv.ParseInt(parts[1], 10, 64)
		if e != nil {
			return 0, ErrProtocol
		}
		if len(parts[1]) == 1 {
			cents *= 10
		}
	}
	return whole*100 + cents, nil
}
