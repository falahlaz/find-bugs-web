package tselcrypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"strings"
	"testing"
)

// testdata/node-fixtures.json was produced by mytsel-tools' lib/tsel-crypto.js
// (itself verified against module-common-function) with throwaway passwords,
// so these tests pin Go to the Node output.
const (
	pkgPassword = "dummy-package-password"
	payPassword = "0123456789abcdef0123456789abcdef"
	payIV       = "fedcba9876543210"
	idPassword  = "test-identifier-password-32bytes"
	identifier  = "628139853636"
	hashsign    = "628139853636|6282234508413|OVO|62888888888|android|9.4.0|1786691964"
)

type bidFixture struct{ Legacy, Scrypt, GCM, Pay string }

func fixtures(t *testing.T) (map[string]bidFixture, map[string]string) {
	t.Helper()
	b, err := os.ReadFile("testdata/node-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	bids, strs := map[string]bidFixture{}, map[string]string{}
	for k, v := range raw {
		if strings.HasPrefix(k, "00") {
			var f bidFixture
			if err := json.Unmarshal(v, &f); err != nil {
				t.Fatal(err)
			}
			bids[k] = f
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			t.Fatal(err)
		}
		strs[k] = s
	}
	return bids, strs
}

func must(t *testing.T, got string, err error, want, label string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s:\n  want %s\n  got  %s", label, want, got)
	}
}

func TestPackageIDMatchesNode(t *testing.T) {
	bids, _ := fixtures(t)
	if len(bids) == 0 {
		t.Fatal("no fixtures")
	}
	for bid, f := range bids {
		got, err := EncryptPackageID(bid, pkgPassword, Legacy)
		must(t, got, err, f.Legacy, "legacy "+bid)
		got, err = EncryptPackageID(bid, pkgPassword, Scrypt)
		must(t, got, err, f.Scrypt, "scrypt "+bid)
		got, err = EncryptPackageIDWithGCM(bid, pkgPassword)
		must(t, got, err, f.GCM, "gcm "+bid)
		got, err = EncryptDeeplinkPayment("PAC|"+f.Legacy, payPassword, payIV)
		must(t, got, err, f.Pay, "payment "+bid)

		got, err = DecryptPackageID(f.Legacy, pkgPassword, Legacy)
		must(t, got, err, bid, "decrypt legacy "+bid)
		got, err = DecryptPackageID(f.Scrypt, pkgPassword, Scrypt)
		must(t, got, err, bid, "decrypt scrypt "+bid)
		got, err = DecryptPackageIDWithGCM(f.GCM, pkgPassword)
		must(t, got, err, bid, "decrypt gcm "+bid)
		got, err = DecryptDeeplinkPayment(f.Pay, payPassword, payIV)
		must(t, got, err, "PAC|"+f.Legacy, "decrypt payment "+bid)
	}
}

func TestWrongPasswordFails(t *testing.T) {
	bids, _ := fixtures(t)
	f := bids["00093370"]
	if _, err := DecryptPackageIDWithGCM(f.GCM, "nope"); err == nil {
		t.Fatal("gcm with wrong password decrypted")
	}
	if _, err := EncryptPackageIDWithGCM("1", ""); err == nil {
		t.Fatal("gcm without password encrypted")
	}
}

func TestIdentifierMatchesNode(t *testing.T) {
	_, s := fixtures(t)
	res, err := DecryptIdentifier(s["v2"], idPassword, nil)
	must(t, res.Plaintext, err, identifier, "v2")
	v3 := "v3::test1" + strings.TrimPrefix(s["v2"], "v2::")
	res, err = DecryptIdentifier(v3, "", map[string]string{"TEST1": idPassword})
	must(t, res.Plaintext, err, identifier, "v3")
	if res.CipherID != "TEST1" {
		t.Fatalf("cipher id %q", res.CipherID)
	}
	res, err = DecryptIdentifier(s["cbc"], idPassword, nil)
	must(t, res.Plaintext, err, identifier, "cbc")

	if _, err := DecryptIdentifier(v3, "", nil); err == nil || !strings.Contains(err.Error(), "no password configured for v3 cipher ID TEST1") {
		t.Fatalf("v3 without password: %v", err)
	}
	if _, err := DecryptIdentifier("v3::ab", "", nil); err == nil {
		t.Fatal("short v3 accepted")
	}
}

// RSA-OAEP/SHA-1 matched Node's publicEncrypt/privateDecrypt when this was
// ported; the key is generated here so no private key lives in the repo.
func TestRSA(t *testing.T) {
	gen, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(gen)
	pems := map[string][]byte{
		"pkcs1": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(gen)}),
		"pkcs8": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}),
	}
	for name, p := range pems {
		key, err := ParsePrivateKey(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		enc, err := RSAEncrypt(hashsign, key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := RSADecrypt(enc, key)
		must(t, got, err, hashsign, "roundtrip "+name)
	}
	if _, err := ParsePrivateKey([]byte("nope")); err == nil {
		t.Fatal("garbage parsed as a key")
	}
}
