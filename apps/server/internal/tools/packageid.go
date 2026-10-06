package tools

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// packageID encrypts a BID in every package-ID format, or recovers one.
// Neither direction asks for the format, because nobody knows it: the same
// password yields three ciphertexts depending on the cipher version and the
// Node version of the service. Encrypt emits all three; decrypt tries all
// three and reports which matched.
var packageID = Tool{
	ID:          "package-id",
	Name:        "Package ID",
	Description: "Encrypt a BID in every package-ID format, or recover the BID from an encrypted one.",
	Docs:        "Mirrors encryptPackageID / decryptPackageID in module-common-function.",
	Fields: []ToolField{
		{Name: "action", Label: "Action", Type: Tabs, Default: "decrypt", Options: []ToolOption{
			{Value: "decrypt", Label: "Decrypt"},
			{Value: "encrypt", Label: "Encrypt"},
		}},
		{Name: "encrypted", Label: "Encrypted package ID", Type: Textarea, Required: true,
			ShowIf: &ToolShowIf{Field: "action", Equals: "decrypt"}, Placeholder: "4a5dfc8d3d6f4a22e41dddf88118488b",
			Help: `Hex, a "v2::" value, or a whole package-details URL — every format is tried.`},
		{Name: "bid", Label: "BID", Required: true, ShowIf: &ToolShowIf{Field: "action", Equals: "encrypt"},
			Placeholder: "00093370", Help: "The business product ID to encrypt."},
		{Name: "environment", Label: "Environment", Type: Select, Default: "other", Options: environmentOptions,
			Help: "Picks the cipher password."},
		{Name: "packagePassword", Label: "Package cipher override", Placeholder: "leave blank to use the environment preset",
			Help: "Overrides PACKAGE_CIPHER_PASSWORD. Required for v2 on the fallback environment."},
	},
	Run: runPackageID,
}

type packageFormat struct {
	id, label string
	encrypt   func(bid, pw string) (string, error)
	decrypt   func(v, pw string) (string, error)
}

// packageFormats are the three formats a package ID is found in, newest first.
var packageFormats = []packageFormat{
	{"v2", `v2 — AES-256-GCM ("v2::" prefix)`, tselcrypto.EncryptPackageIDWithGCM, tselcrypto.DecryptPackageIDWithGCM},
	{"v1-legacy", "v1 — AES-256-CBC, legacy EVP_BytesToKey (services on Node <= 21)",
		func(b, p string) (string, error) { return tselcrypto.EncryptPackageID(b, p, tselcrypto.Legacy) },
		func(v, p string) (string, error) { return tselcrypto.DecryptPackageID(v, p, tselcrypto.Legacy) }},
	{"v1-scrypt", "v1 — AES-256-CBC, scrypt patch (services on Node >= 22)",
		func(b, p string) (string, error) { return tselcrypto.EncryptPackageID(b, p, tselcrypto.Scrypt) },
		func(v, p string) (string, error) { return tselcrypto.DecryptPackageID(v, p, tselcrypto.Scrypt) }},
}

var (
	anyHex    = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	printable = regexp.MustCompile(`^[\x20-\x7e]+$`)
	urlSplit  = regexp.MustCompile(`[?#]`)
	httpURL   = regexp.MustCompile(`(?i)^https?://`)
)

// extractCiphertext accepts a bare hex string, a "v2::" value, or a whole
// .../app/package-details/<value>?campaignId=... URL.
func extractCiphertext(raw string) string {
	v := strings.TrimSpace(raw)
	if httpURL.MatchString(v) || strings.Contains(v, "/") {
		segs := strings.FieldsFunc(urlSplit.Split(v, 2)[0], func(r rune) bool { return r == '/' })
		v = ""
		if len(segs) > 0 {
			v = segs[len(segs)-1]
		}
	}
	if d, err := url.PathUnescape(v); err == nil {
		return d
	}
	return v
}

// skipReason rules out a format up front, so the attempts table says "too
// short to be GCM" rather than a low-level cipher error.
func skipReason(f packageFormat, v string) string {
	payload := strings.TrimPrefix(v, "v2::")
	if !anyHex.MatchString(payload) {
		return "not hexadecimal"
	}
	if len(payload)%2 != 0 {
		return "odd number of hex digits"
	}
	n := len(payload) / 2
	if f.id == "v2" {
		if n <= 16 {
			return "too short to hold a 16-byte auth tag"
		}
		return ""
	}
	if strings.HasPrefix(v, "v2::") {
		return "input is tagged v2::"
	}
	if n%16 != 0 {
		return "not a multiple of the 16-byte AES block"
	}
	return ""
}

type attempt struct {
	f         packageFormat
	plaintext string
	reason    string // empty when it decrypted
}

func tryFormat(f packageFormat, v, pw string) attempt {
	if r := skipReason(f, v); r != "" {
		return attempt{f: f, reason: r}
	}
	// Hex is case-insensitive in Node's Buffer.from; keep that.
	pt, err := f.decrypt(strings.ToLower(v), pw)
	if err != nil {
		return attempt{f: f, reason: err.Error()}
	}
	if !printable.MatchString(pt) {
		return attempt{f: f, reason: "decrypted to non-printable bytes (wrong password)"}
	}
	return attempt{f: f, plaintext: pt}
}

func runPackageID(s *Service, in Input) (ToolResult, error) {
	env := in.Str("environment")
	p, err := s.preset(env)
	if err != nil {
		return ToolResult{}, err
	}
	override := in.Str("packagePassword")
	pw := override
	if pw == "" {
		pw = p.PackagePassword
	}
	if pw == "" {
		return ToolResult{}, s.missingPasswordErr(env, p)
	}
	cipherRow := ToolSummaryItem{Label: "Package cipher", Value: withOverride(pw, override != ""), Secret: true}

	if in.Str("action") == "encrypt" {
		// The fallback preset means "unset", which GCM has no default for.
		canV2 := env != "fallback" || override != ""
		bid := in.Str("bid")
		var res ToolResult
		for _, f := range packageFormats {
			if f.id == "v2" && !canV2 {
				continue
			}
			v, err := f.encrypt(bid, pw)
			if err != nil {
				return ToolResult{}, userErr("Encrypt failed: %v", err)
			}
			res.Outputs = append(res.Outputs, ToolOutput{Label: f.label, Value: v, Kind: "code"})
		}
		res.Warnings = []string{"v1 comes in two flavours: legacy matches services on Node 21 or older, scrypt matches Node 22 or newer. If a value is rejected, use the other one."}
		if !canV2 {
			res.Warnings = append(res.Warnings, "v2 (GCM) skipped: it has no fallback password in module-common-function. Set a package cipher override to get one.")
		}
		res.Summary = []ToolSummaryItem{{Label: "Environment", Value: p.Label}, {Label: "BID", Value: bid}, cipherRow}
		return res, nil
	}

	v := extractCiphertext(in.Str("encrypted"))
	if v == "" {
		return ToolResult{}, userErr("Nothing to decrypt — the input has no ciphertext in it.")
	}
	var attempts, hits []attempt
	for _, f := range packageFormats {
		a := tryFormat(f, v, pw)
		attempts = append(attempts, a)
		if a.reason == "" {
			hits = append(hits, a)
		}
	}
	if len(hits) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "No format decrypted this value with the %q password.\n", env)
		for _, a := range attempts {
			fmt.Fprintf(&b, "  %s: %s\n", a.f.label, a.reason)
		}
		b.WriteString("Check the environment, or set a package cipher override.")
		return ToolResult{}, userErr("%s", b.String())
	}

	matched := make([]string, len(hits))
	var res ToolResult
	for i, h := range hits {
		matched[i] = h.f.label
		res.Outputs = append(res.Outputs, ToolOutput{Label: "BID (" + h.f.label + ")", Value: h.plaintext, Kind: "code"})
	}
	res.Summary = []ToolSummaryItem{
		{Label: "Environment", Value: p.Label},
		{Label: "Ciphertext", Value: v},
		{Label: "Matched", Value: strings.Join(matched, ", ")},
		cipherRow,
	}
	for _, a := range attempts {
		val := a.reason
		if val == "" {
			val = "ok → " + a.plaintext
		}
		res.Summary = append(res.Summary, ToolSummaryItem{Label: a.f.label, Value: val})
	}
	if len(hits) > 1 {
		res.Warnings = []string{"More than one format decrypted this value, so the result is ambiguous. Check which service produced it."}
	}
	return res, nil
}
