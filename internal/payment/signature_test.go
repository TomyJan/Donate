package payment

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"github.com/TomyJan/Donate/internal/config"
	"maps"
	"testing"
	"time"
)

func TestV1SignatureRejectsTamperAndWrongAlgorithm(t *testing.T) {
	g := &Gateway{Config: config.Config{EpayKey: "secret"}}
	sum := md5.Sum([]byte("money=0.01&pid=1001secret"))
	v := map[string]string{"money": "0.01", "pid": "1001", "sign_type": "MD5", "sign": hex.EncodeToString(sum[:])}
	if e := g.verify(v, "v1"); e != nil {
		t.Fatal(e)
	}
	for _, change := range []map[string]string{{"money": "1.00"}, {"sign_type": "RSA"}, {"sign": ""}} {
		bad := maps.Clone(v)
		maps.Copy(bad, change)
		if e := g.verify(bad, "v1"); e == nil {
			t.Fatal("bad signature accepted")
		}
	}
	g.Config.EpayKey = ""
	if e := g.verify(v, "v1"); e == nil {
		t.Fatal("missing key accepted")
	}
}
func TestV2SignatureRejectsStaleFutureAndMalformedInput(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	g := &Gateway{public: &key.PublicKey, Now: func() time.Time { return time.Unix(1793318400, 0) }}
	v := map[string]string{"pid": "1001", "timestamp": "1793318400", "sign_type": "RSA"}
	h := sha256.Sum256([]byte("pid=1001&timestamp=1793318400"))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if e != nil {
		t.Fatal(e)
	}
	v["sign"] = base64.StdEncoding.EncodeToString(sig)
	if e = g.verify(v, "v2"); e != nil {
		t.Fatal(e)
	}
	for _, change := range []map[string]string{{"timestamp": "1793318099"}, {"timestamp": "1793318701"}, {"timestamp": "nan"}, {"sign": "!invalid"}, {"sign_type": "MD5"}, {"pid": "other"}} {
		bad := maps.Clone(v)
		maps.Copy(bad, change)
		if e = g.verify(bad, "v2"); e == nil {
			t.Fatal("invalid signature accepted", change)
		}
	}
	if e = g.verify(v, "unknown"); e == nil {
		t.Fatal("unknown protocol accepted")
	}
	g.public = nil
	if e = g.verify(v, "v2"); e == nil {
		t.Fatal("missing public key accepted")
	}
}
func TestMoneyFormatsAndRejectsMalformedGatewayValues(t *testing.T) {
	for _, value := range []string{"", ".1", "1.", "1.001", "1.0.0", "-1", "1e2", "1.x", "1000000000"} {
		if _, e := ParseMoney(value); e == nil {
			t.Fatal(value)
		}
	}
	for _, fen := range []int64{1, 10, 888, 10000000} {
		got, e := ParseMoney(Money(fen))
		if e != nil || got != fen {
			t.Fatal(fen, got, e)
		}
	}
}
