package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// tokenDecrypt decrypts what the web API receives from the browser: the
// identity cookies (decryptCookies / decryptIdentifier) and the profilePlan
// header (decryptWithPrivateKey, same private.pem as hashsign).
//
//	v3::<cipherId><hex>   AES-256-GCM + gzip, password per 5-char cipher ID
//	v2::<hex>             AES-256-GCM + gzip, IDENTIFIER_AUTH_CIPHER_PASSWORD
//	<iv hex>-<hex>        AES-256-CBC, same password, no gzip
var tokenDecrypt = Tool{
	ID:          "token-decrypt",
	Name:        "Token Decrypt",
	Description: "Decrypt the identity cookies (identifierEnc, serviceEnc, defaultIdentifier) or a profilePlan header.",
	Docs:        "Mirrors decryptCookies / decryptIdentifier and decryptWithPrivateKey in module-common-function.",
	Fields: []ToolField{
		{Name: "action", Label: "Decrypt", Type: Tabs, Default: "cookie", Options: []ToolOption{
			{Value: "cookie", Label: "Cookie"},
			{Value: "profileplan", Label: "profilePlan"},
		}},
		{Name: "cookie", Label: "Cookie header", Type: Textarea, Required: true, ShowIf: &ToolShowIf{Field: "action", Equals: "cookie"},
			Placeholder: "identifierEnc=v2::…; serviceEnc=v2::…; defaultIdentifier=v2::…",
			Help:        "The whole Cookie header, or one encrypted value on its own. v2::, v3:: and legacy CBC are all handled."},
		{Name: "profilePlan", Label: "profilePlan header", Type: Textarea, Required: true, ShowIf: &ToolShowIf{Field: "action", Equals: "profileplan"},
			Placeholder: "base64 value of the profilePlan header",
			Help:        "Decrypted with the services' private.pem, the same key the hashsign tool uses."},
		{Name: "environment", Label: "Environment", Type: Select, Default: "other", ShowIf: &ToolShowIf{Field: "action", Equals: "cookie"},
			Options: environmentOptions, Help: "Picks the identifier passwords."},
		{Name: "identifierPassword", Label: "Identifier password override", ShowIf: &ToolShowIf{Field: "action", Equals: "cookie"},
			Placeholder: "leave blank to use the environment preset", Group: "overrides",
			Help: "Overrides IDENTIFIER_AUTH_CIPHER_PASSWORD, used by v2 and legacy CBC. Must be 32 characters."},
		{Name: "v3Password", Label: "v3 password override", ShowIf: &ToolShowIf{Field: "action", Equals: "cookie"},
			Placeholder: "leave blank to use the environment preset", Group: "overrides",
			Help: "Overrides IDENTIFIER_AUTH_CIPHER_PASSWORD_<CIPHERID> for every v3 value pasted."},
	},
	Run: runTokenDecrypt,
}

// cookieNames are the cookies decryptCookies reads, plus defaultIdentifier
// which uses the same cipher.
var cookieNames = []string{"identifierEnc", "serviceEnc", "defaultIdentifier"}

var formatLabels = map[string]string{
	"v3":  "v3 — AES-256-GCM, per cipher ID",
	"v2":  "v2 — AES-256-GCM",
	"cbc": "legacy — AES-256-CBC",
}

func safeDecode(v string) string {
	if d, err := url.PathUnescape(v); err == nil {
		return d
	}
	return v
}

type cookie struct{ name, value string }

var cookiePrefix = regexp.MustCompile(`(?i)^cookie:\s*`)

// parseCookies accepts a whole Cookie header (with or without "Cookie:") or
// a single bare value copied out of devtools.
func parseCookies(raw string) []cookie {
	text := cookiePrefix.ReplaceAllString(strings.TrimSpace(raw), "")
	if !strings.ContainsAny(text, "=;") {
		return []cookie{{"Value", safeDecode(text)}}
	}
	var out []cookie
	for _, pair := range strings.Split(text, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		value = safeDecode(value)
		if value != "" && slices.Contains(cookieNames, name) {
			out = append(out, cookie{name, value})
		}
	}
	return out
}

func v3ID(v string) string {
	rest := strings.TrimPrefix(v, "v3::")
	return strings.ToUpper(rest[:min(5, len(rest))])
}

func runTokenDecrypt(s *Service, in Input) (ToolResult, error) {
	if in.Str("action") == "profileplan" {
		return s.decryptProfilePlan(in)
	}
	cookies := parseCookies(in.Str("cookie"))
	if len(cookies) == 0 {
		return ToolResult{}, userErr("No %s cookie found. Paste the whole Cookie header or a single encrypted value.", strings.Join(cookieNames, " / "))
	}
	env := in.Str("environment")
	p, err := s.preset(env)
	if err != nil {
		return ToolResult{}, err
	}
	override := in.Str("identifierPassword")
	pw := override
	if pw == "" {
		pw = p.IdentifierPassword
	}
	// An override applies to whichever cipher IDs the pasted cookies use.
	v3 := map[string]string{}
	for k, v := range p.IdentifierV3Passwords {
		v3[k] = v
	}
	v3Override := in.Str("v3Password")
	if v3Override != "" {
		for _, c := range cookies {
			if strings.HasPrefix(c.value, "v3::") {
				v3[v3ID(c.value)] = v3Override
			}
		}
	}

	type outcome struct {
		c   cookie
		id  tselcrypto.Identifier
		err string
	}
	var ok, failed []outcome
	for _, c := range cookies {
		if !strings.HasPrefix(c.value, "v3::") && pw == "" {
			failed = append(failed, outcome{c: c, err: fmt.Sprintf("no identifier password configured for %q. Set identifierPassword in %s or fill in the override.", env, s.CiphersFile)})
			continue
		}
		id, err := tselcrypto.DecryptIdentifier(c.value, pw, v3)
		if err != nil {
			failed = append(failed, outcome{c: c, err: err.Error()})
			continue
		}
		ok = append(ok, outcome{c: c, id: id})
	}
	if len(ok) == 0 {
		var b strings.Builder
		b.WriteString("Nothing decrypted.\n")
		for _, f := range failed {
			fmt.Fprintf(&b, "  %s: %s\n", f.c.name, f.err)
		}
		b.WriteString("Check the environment, or set a password override.")
		return ToolResult{}, userErr("%s", b.String())
	}

	res := ToolResult{Summary: []ToolSummaryItem{{Label: "Environment", Value: p.Label}}}
	var used []string
	for _, o := range ok {
		label := formatLabels[o.id.Version]
		if o.id.Version == "v3" {
			label += " (" + o.id.CipherID + ")"
			if !slices.Contains(used, o.id.CipherID) {
				used = append(used, o.id.CipherID)
			}
		}
		res.Summary = append(res.Summary, ToolSummaryItem{Label: o.c.name, Value: label})
		res.Outputs = append(res.Outputs, ToolOutput{Label: o.c.name, Value: o.id.Plaintext, Kind: "code"})
	}
	if pw != "" {
		res.Summary = append(res.Summary, ToolSummaryItem{Label: "Identifier password", Value: withOverride(pw, override != ""), Secret: true})
	}
	for _, id := range used {
		res.Summary = append(res.Summary, ToolSummaryItem{Label: "v3 password (" + id + ")", Value: withOverride(v3[id], v3Override != ""), Secret: true})
	}
	for _, f := range failed {
		res.Warnings = append(res.Warnings, f.c.name+": decrypt failed — "+f.err)
	}
	return res, nil
}

var profilePlanPrefix = regexp.MustCompile(`(?i)^profileplan:\s*`)

func (s *Service) decryptProfilePlan(in Input) (ToolResult, error) {
	v := profilePlanPrefix.ReplaceAllString(strings.TrimSpace(in.Str("profilePlan")), "")
	key, err := s.privateKey()
	if err != nil {
		return ToolResult{}, err
	}
	if strings.Contains(v, "%") {
		v = safeDecode(v)
	}
	pt, err := tselcrypto.RSADecrypt(v, key)
	if err != nil {
		return ToolResult{}, userErr("Decrypt failed: %v\nEither this is not a profilePlan value, it was cut off when copying, or it was encrypted for a different key.", err)
	}
	pretty := pt
	var buf bytes.Buffer
	if json.Valid([]byte(pt)) && json.Indent(&buf, []byte(pt), "", "  ") == nil {
		pretty = buf.String()
	}
	return ToolResult{
		Summary: []ToolSummaryItem{{Label: "Length", Value: fmt.Sprintf("%d characters", len([]rune(pt)))}},
		Outputs: []ToolOutput{{Label: "profilePlan", Value: pretty, Kind: "code"}},
	}, nil
}
