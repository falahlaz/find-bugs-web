package tools

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// Throwaway values; the Node fixtures in tselcrypto/testdata use the same.
const (
	pkgPW = "dummy-package-password"
	payPW = "0123456789abcdef0123456789abcdef"
	payIV = "fedcba9876543210"
	idPW  = "test-identifier-password-32bytes"
	now   = 1786691964
)

func newTestService(t *testing.T) (*Service, *rsa.PrivateKey) {
	t.Helper()
	dir := t.TempDir()
	env := map[string]any{"packagePassword": pkgPW, "paymentPassword": payPW, "paymentIv": payIV,
		"identifierPassword": idPW, "identifierV3Passwords": map[string]string{"test1": idPW}}
	b, _ := json.Marshal(map[string]any{"production": env, "other": env})
	ciphers := filepath.Join(dir, "ciphers.json")
	if err := os.WriteFile(ciphers, b, 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "private.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Jakarta")
	s := New(ciphers, keyFile, loc)
	s.Now = func() time.Time { return time.Unix(now, 0) }
	return s, key
}

func output(t *testing.T, r ToolResult, label string) string {
	t.Helper()
	for _, o := range r.Outputs {
		if o.Label == label {
			return o.Value
		}
	}
	t.Fatalf("no output %q in %+v", label, r.Outputs)
	return ""
}

func run(t *testing.T, s *Service, id string, in map[string]any) ToolResult {
	t.Helper()
	r, err := s.Run(id, in)
	if err != nil {
		t.Fatalf("%s %v: %v", id, in, err)
	}
	return r
}

func wantErr[E error](t *testing.T, s *Service, id string, in map[string]any) {
	t.Helper()
	_, err := s.Run(id, in)
	var target E
	if !errors.As(err, &target) {
		t.Fatalf("%s %v: want %T, got %v", id, in, target, err)
	}
}

func TestManifest(t *testing.T) {
	s, _ := newTestService(t)
	var ids []string
	for _, ti := range s.List() {
		ids = append(ids, ti.ID)
		for _, f := range ti.Fields {
			if f.Type == "" || f.Label == "" {
				t.Fatalf("%s.%s not defaulted", ti.ID, f.Name)
			}
		}
	}
	if want := []string{"payment-deeplink", "hashsign", "package-id", "token-decrypt"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids %v", ids)
	}
}

func TestNormalise(t *testing.T) {
	s, _ := newTestService(t)
	wantErr[*InputError](t, s, "payment-deeplink", map[string]any{"environment": "other"})
	wantErr[*InputError](t, s, "payment-deeplink", map[string]any{"bid": "1", "environment": "nonsense"})
	wantErr[*InputError](t, s, "hashsign", map[string]any{"action": "nonsense"})
	if _, err := s.Run("nope", nil); !errors.Is(err, ErrUnknownTool) {
		t.Fatal(err)
	}

	visible := func(vals map[string]any) []string {
		var out []string
		fields := hashsignTool.Fields
		active := ActiveFields(fields, vals)
		for _, f := range fields {
			if active[f.Name] {
				out = append(out, f.Name)
			}
		}
		return out
	}
	if got := visible(map[string]any{"action": "decrypt", "bumpMode": "now", "inputMode": "fields"}); !reflect.DeepEqual(got, []string{"action", "hashsign"}) {
		t.Fatalf("decrypt shows %v", got)
	}
	// bumpMode is hidden on "encrypt", so its stale "offset" mustn't show offsetDays.
	if got := visible(map[string]any{"action": "encrypt", "bumpMode": "offset", "inputMode": "raw"}); !reflect.DeepEqual(got, []string{"action", "inputMode", "plaintext"}) {
		t.Fatalf("encrypt shows %v", got)
	}
	in, problems := Normalise(hashsignTool.Fields, map[string]any{"action": "bump", "hashsign": " x ", "bumpMode": "offset", "offsetDays": "7", "offsetMinutes": 1.5})
	if len(problems) > 0 || in.Str("hashsign") != "x" {
		t.Fatalf("%v %v", in, problems)
	}
	if d, _ := in.Num("offsetDays"); d != 7 {
		t.Fatal(d)
	}
	if _, problems := Normalise(hashsignTool.Fields, map[string]any{"action": "bump", "hashsign": "x", "bumpMode": "offset", "offsetDays": "abc"}); len(problems) != 1 {
		t.Fatal(problems)
	}
}

func TestDeeplink(t *testing.T) {
	s, _ := newTestService(t)
	// Expected values come from mytsel-tools (tselcrypto/testdata).
	r := run(t, s, "payment-deeplink", map[string]any{"bid": "00093370", "environment": "production", "linkType": "both"})
	if got := output(t, r, "Package detail link"); got != "https://my.telkomsel.com/app/package-details/c0edec74b3ac7817454b9cb9b6037d11" {
		t.Fatal(got)
	}
	if got := output(t, r, "Payment method link"); got != "https://my.telkomsel.com/app/payment-method?link=df9d368ffdda68180873c3284e1cd21e808588002151eaeb23b165b2931e9e6a90890f88e2bc6ad91523fbeae6aab901" {
		t.Fatal(got)
	}
	r = run(t, s, "payment-deeplink", map[string]any{"bid": "00093370", "linkType": "package-detail", "keyDerivation": "scrypt", "campaignId": "c1"})
	if got := output(t, r, "Package detail link"); got != "https://tdwpreweb.telkomsel.com/app/package-details/62c4a293bbbb57c607b8e52a37fed52c?campaignId=c1&campaignTrackingId=" {
		t.Fatal(got)
	}
	r = run(t, s, "payment-deeplink", map[string]any{"bid": "00093370", "linkType": "package-detail", "encryption": "v2"})
	if got := output(t, r, "Package detail link"); !strings.HasSuffix(got, "/v2::91b5e60870ec2d2a21207ce8694a831d11470a11e239540a") {
		t.Fatal(got)
	}
	// Campaign fields are hidden for payment-method only.
	r = run(t, s, "payment-deeplink", map[string]any{"bid": "1", "linkType": "payment-method", "campaignId": "c1"})
	if len(r.Outputs) != 1 || strings.Contains(r.Outputs[0].Value, "campaign") {
		t.Fatal(r.Outputs)
	}
	wantErr[*UserError](t, s, "payment-deeplink", map[string]any{"bid": "1", "environment": "fallback", "encryption": "v2", "packagePassword": ""})
	wantErr[*UserError](t, s, "payment-deeplink", map[string]any{"bid": "1", "environment": "fallback"})
}

func TestPackageID(t *testing.T) {
	s, _ := newTestService(t)
	r := run(t, s, "package-id", map[string]any{"action": "encrypt", "bid": "00093370", "environment": "production"})
	if len(r.Outputs) != 3 || r.Outputs[1].Value != "c0edec74b3ac7817454b9cb9b6037d11" || r.Outputs[2].Value != "62c4a293bbbb57c607b8e52a37fed52c" {
		t.Fatal(r.Outputs)
	}
	for _, o := range r.Outputs {
		d := run(t, s, "package-id", map[string]any{"action": "decrypt", "encrypted": o.Value, "environment": "production"})
		if len(d.Outputs) != 1 || d.Outputs[0].Value != "00093370" {
			t.Fatalf("%s → %v", o.Label, d.Outputs)
		}
	}
	d := run(t, s, "package-id", map[string]any{"action": "decrypt",
		"encrypted": "https://my.telkomsel.com/app/package-details/C0EDEC74B3AC7817454B9CB9B6037D11?campaignId=x"})
	if d.Outputs[0].Value != "00093370" {
		t.Fatal(d.Outputs)
	}
	wantErr[*UserError](t, s, "package-id", map[string]any{"action": "decrypt", "encrypted": "c0edec74b3ac7817454b9cb9b6037d11", "packagePassword": "wrong"})
	wantErr[*UserError](t, s, "package-id", map[string]any{"action": "decrypt", "encrypted": "not-a-ciphertext"})

	r = run(t, s, "package-id", map[string]any{"action": "encrypt", "bid": "1", "environment": "fallback", "packagePassword": "x"})
	if len(r.Outputs) != 3 {
		t.Fatal(r.Outputs)
	}
}

func gcmIdentifier(t *testing.T, plaintext string) string {
	t.Helper()
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	zw.Write([]byte(plaintext))
	zw.Close()
	block, _ := aes.NewCipher([]byte(idPW))
	g, _ := cipher.NewGCM(block)
	iv := make([]byte, 12)
	rand.Read(iv)
	return hex.EncodeToString(g.Seal(iv, iv, z.Bytes(), nil))
}

func cbcIdentifier(t *testing.T, plaintext string) string {
	t.Helper()
	block, _ := aes.NewCipher([]byte(idPW))
	iv := make([]byte, 16)
	rand.Read(iv)
	n := 16 - len(plaintext)%16
	buf := append([]byte(plaintext), bytes.Repeat([]byte{byte(n)}, n)...)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(buf, buf)
	return hex.EncodeToString(iv) + "-" + hex.EncodeToString(buf)
}

func TestTokenDecrypt(t *testing.T) {
	s, key := newTestService(t)
	header := "Cookie: foo=bar; identifierEnc=v2::" + gcmIdentifier(t, "628139853636") +
		"; serviceEnc=v3::TEST1" + gcmIdentifier(t, "6282234508413") +
		"; defaultIdentifier=" + cbcIdentifier(t, "62888888888")
	r := run(t, s, "token-decrypt", map[string]any{"cookie": header})
	if output(t, r, "identifierEnc") != "628139853636" || output(t, r, "serviceEnc") != "6282234508413" ||
		output(t, r, "defaultIdentifier") != "62888888888" || len(r.Outputs) != 3 {
		t.Fatal(r.Outputs)
	}
	r = run(t, s, "token-decrypt", map[string]any{"cookie": strings.ReplaceAll("v2::"+gcmIdentifier(t, "628139853636"), ":", "%3A")})
	if output(t, r, "Value") != "628139853636" {
		t.Fatal(r.Outputs)
	}
	r = run(t, s, "token-decrypt", map[string]any{"cookie": "identifierEnc=v2::" + gcmIdentifier(t, "6281") + "; serviceEnc=v3::ZZZZZ" + gcmIdentifier(t, "x")})
	if output(t, r, "identifierEnc") != "6281" || len(r.Warnings) != 1 {
		t.Fatal(r)
	}
	wantErr[*UserError](t, s, "token-decrypt", map[string]any{"cookie": "identifierEnc=v2::" + gcmIdentifier(t, "1"), "identifierPassword": strings.Repeat("x", 32)})
	wantErr[*UserError](t, s, "token-decrypt", map[string]any{"cookie": "a=1; b=2"})

	plan, _ := tselcrypto.RSAEncrypt(`{"a":1}`, key)
	r = run(t, s, "token-decrypt", map[string]any{"action": "profileplan", "profilePlan": "profilePlan: " + plan})
	if got := output(t, r, "profilePlan"); got != "{\n  \"a\": 1\n}" {
		t.Fatal(got)
	}
	wantErr[*UserError](t, s, "token-decrypt", map[string]any{"action": "profileplan", "profilePlan": "not-a-ciphertext"})
}

func TestHashsign(t *testing.T) {
	s, _ := newTestService(t)
	plain := "628139853636|6282234508413|OVO|62888888888|android|9.4.0|" + strconv.Itoa(now-60)
	r := run(t, s, "hashsign", map[string]any{"action": "encrypt", "inputMode": "raw", "plaintext": plain})
	hs := output(t, r, "hashsign")
	if len(r.Warnings) != 0 {
		t.Fatal(r.Warnings)
	}
	r = run(t, s, "hashsign", map[string]any{"action": "decrypt", "hashsign": hs})
	if output(t, r, "Plaintext") != plain {
		t.Fatal(r.Outputs)
	}

	ts := func(in map[string]any) string {
		in["action"], in["hashsign"] = "bump", hs
		p := strings.Split(output(t, run(t, s, "hashsign", in), "Plaintext"), "|")
		return p[6]
	}
	if got := ts(map[string]any{"bumpMode": "now"}); got != strconv.Itoa(now) {
		t.Fatal(got)
	}
	if got := ts(map[string]any{"bumpMode": "offset", "offsetDays": 7}); got != strconv.Itoa(now-60+7*86400) {
		t.Fatal(got)
	}
	if got := ts(map[string]any{"bumpMode": "absolute", "targetTime": "1913022000"}); got != "1913022000" {
		t.Fatal(got)
	}
	// No timezone → WIB.
	if got := ts(map[string]any{"bumpMode": "absolute", "targetTime": "2026-08-14T14:00:00"}); got != "1786690800" {
		t.Fatal(got)
	}
	if got := ts(map[string]any{"bumpMode": "absolute", "targetTime": "2026-08-14T14:00:00+07:00"}); got != "1786690800" {
		t.Fatal(got)
	}
	wantErr[*UserError](t, s, "hashsign", map[string]any{"action": "bump", "hashsign": hs, "bumpMode": "offset"})
	wantErr[*UserError](t, s, "hashsign", map[string]any{"action": "bump", "hashsign": hs, "bumpMode": "absolute", "targetTime": "soon"})

	r = run(t, s, "hashsign", map[string]any{"action": "encrypt", "field_msisdn": "62811", "field_paymentMethod": "OVO"})
	if got := output(t, r, "Plaintext"); got != "62811||OVO||||"+strconv.Itoa(now) {
		t.Fatal(got)
	}
	r = run(t, s, "hashsign", map[string]any{"action": "encrypt", "inputMode": "raw", "plaintext": "a|b|c|d|e|f|1"})
	if len(r.Warnings) != 1 {
		t.Fatal("expired timestamp not flagged", r.Warnings)
	}
	wantErr[*UserError](t, s, "hashsign", map[string]any{"action": "decrypt", "hashsign": "bm9wZQ=="})

	s.PrivateKeyFile = filepath.Join(t.TempDir(), "missing.pem")
	wantErr[*UserError](t, s, "hashsign", map[string]any{"action": "decrypt", "hashsign": hs})
}
