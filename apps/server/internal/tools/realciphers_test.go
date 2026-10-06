package tools

import (
	"os"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// realFixtures were captured from module-common-function with the real cipher
// passwords (mytsel-tools test/crypto-parity.test.js): legacy on Node 20.9.0,
// scrypt on Node 25.1.0. They only run when TSEL_CIPHERS_FILE points at the
// real ciphers, so a wrong config shows up as a mismatch, not a silent pass:
//
//	TSEL_CIPHERS_FILE=~/.config/findbugs/tsel-ciphers.json go test ./internal/tools/ -run Real
var realFixtures = map[string]map[string][2]string{
	"production": {
		"00093370": {"4a5dfc8d3d6f4a22e41dddf88118488b", "f00196cc4812c2cf23ef5502a51cc6a5"},
		"00141273": {"338e855d21414b7be73e96a62aeef8bd", "5960d3bda58caf378fefb62032b5deb2"},
	},
	"other": {
		"00093370": {"d8bc618a18a975d5092b730c9c362273", "10199e9c4b50593139f48a1d95f1d8b8"},
		"00141273": {"a3d241e57f395ac9bf605aa95b5e3878", "384891f5e67fe5370a57bfc196ef3657"},
	},
	"fallback": {
		"00093370": {"12b9422dc7efaf90a068cd0bf993a2db", "f25ffd1022f7fa56a8aa475cfba73aaa"},
		"00141273": {"81388939215bbd2c1b34a39b4439f0a4", "211dcb7ae9f6d43cb52116fc2e5b9a9c"},
	},
}

var realGCMFixtures = map[string]string{
	"production": "v2::e89309218ca0efab9771f4ef234226636ee4aa62abc9af72",
	"other":      "v2::4715b0a0572c7f92d28c0067289374b4f212d28a97c6535f",
	"fallback":   "v2::9d63fc1dcf4dd255171e1e1a122b33feae9b5f859f6c0bea",
}

func TestRealCiphersMatchModuleCommonFunction(t *testing.T) {
	file := os.Getenv("TSEL_CIPHERS_FILE")
	if file == "" {
		t.Skip("TSEL_CIPHERS_FILE not set")
	}
	s := New(file, os.Getenv("TSEL_PRIVATE_KEY_PATH"), nil)
	presets, err := s.ciphers()
	if err != nil {
		t.Fatal(err)
	}
	for env, bids := range realFixtures {
		p := presets[env]
		if len(p.Missing) > 0 {
			t.Fatalf("%s missing %v", env, p.Missing)
		}
		for bid, want := range bids {
			if got, _ := tselcrypto.EncryptPackageID(bid, p.PackagePassword, tselcrypto.Legacy); got != want[0] {
				t.Errorf("%s %s legacy mismatch", env, bid)
			}
			if got, _ := tselcrypto.EncryptPackageID(bid, p.PackagePassword, tselcrypto.Scrypt); got != want[1] {
				t.Errorf("%s %s scrypt mismatch", env, bid)
			}
		}
		if got, _ := tselcrypto.EncryptPackageIDWithGCM("00093370", p.PackagePassword); got != realGCMFixtures[env] {
			t.Errorf("%s gcm mismatch", env)
		}
		link, err := tselcrypto.EncryptDeeplinkPayment("PAC|x", p.PaymentPassword, p.PaymentIV)
		if err != nil {
			t.Errorf("%s payment cipher: %v", env, err)
		} else if back, _ := tselcrypto.DecryptDeeplinkPayment(link, p.PaymentPassword, p.PaymentIV); back != "PAC|x" {
			t.Errorf("%s payment roundtrip", env)
		}
	}
	if s.PrivateKeyFile != "" {
		key, err := s.privateKey()
		if err != nil {
			t.Fatal(err)
		}
		const plain = "628139853636|6282234508413|OVO|62888888888|android|9.4.0|1786691964"
		hs, _ := tselcrypto.RSAEncrypt(plain, key)
		if back, err := tselcrypto.RSADecrypt(hs, key); err != nil || back != plain {
			t.Errorf("rsa roundtrip: %v", err)
		}
	}
}
