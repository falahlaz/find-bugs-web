package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// The cipher passwords are real production credentials, so they live in a
// git-ignored JSON file (TSEL_CIPHERS_FILE, same shape as mytsel-tools'
// config/ciphers.json). Any entry can also come from the environment, which
// wins over the file: TSEL_PRODUCTION_PACKAGE_PASSWORD, TSEL_OTHER_PAYMENT_IV,
// TSEL_<ENV>_IDENTIFIER_PASSWORD[_<CIPHERID>], ...

var environmentNames = []string{"production", "other", "fallback"}

var environmentOptions = []ToolOption{
	{Value: "production", Label: "Production"},
	{Value: "other", Label: "Non-production"},
	{Value: "fallback", Label: "Fallback (unset env vars)"},
}

// Non-secret, so these stay in the source.
var webUIURLs = map[string]string{
	"production": "https://my.telkomsel.com",
	"other":      "https://tdwpreweb.telkomsel.com",
	"fallback":   "https://my.telkomsel.com",
}

var envLabels = map[string]string{
	"production": "Production",
	"other":      "Non-production",
	"fallback":   "Fallback (module-common-function defaults)",
}

// Preset is one environment's ciphers.
type Preset struct {
	Label           string
	WebUIURL        string
	PackagePassword string
	PaymentPassword string
	PaymentIV       string
	// IdentifierPassword (v2 and CBC cookies) and IdentifierV3Passwords (by
	// upper-case cipher ID) are optional: only token-decrypt needs them.
	IdentifierPassword    string
	IdentifierV3Passwords map[string]string
	// Missing lists the required keys with no value.
	Missing []string
}

type cipherFileEnv struct {
	PackagePassword       string            `json:"packagePassword"`
	PaymentPassword       string            `json:"paymentPassword"`
	PaymentIV             string            `json:"paymentIv"`
	IdentifierPassword    string            `json:"identifierPassword"`
	IdentifierV3Passwords map[string]string `json:"identifierV3Passwords"`
}

func envVar(env, suffix string) string {
	return "TSEL_" + strings.ToUpper(env) + "_" + suffix
}

// ciphers merges the file and the environment. A missing file is fine (the
// environment may supply everything); a malformed one is an error.
func (s *Service) ciphers() (map[string]Preset, error) {
	file := map[string]cipherFileEnv{}
	if b, err := os.ReadFile(s.CiphersFile); err == nil {
		if err := json.Unmarshal(b, &file); err != nil {
			return nil, userErr("%s is not valid JSON: %v", s.CiphersFile, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read ciphers: %w", err)
	}

	out := map[string]Preset{}
	for _, name := range environmentNames {
		f := file[name]
		p := Preset{Label: envLabels[name], WebUIURL: webUIURLs[name], IdentifierV3Passwords: map[string]string{}}
		pick := func(suffix, fromFile string) string {
			if v := os.Getenv(envVar(name, suffix)); v != "" {
				return v
			}
			return fromFile
		}
		p.PackagePassword = pick("PACKAGE_PASSWORD", f.PackagePassword)
		p.PaymentPassword = pick("PAYMENT_PASSWORD", f.PaymentPassword)
		p.PaymentIV = pick("PAYMENT_IV", f.PaymentIV)
		p.IdentifierPassword = pick("IDENTIFIER_PASSWORD", f.IdentifierPassword)
		for _, kv := range [][2]string{{"packagePassword", p.PackagePassword}, {"paymentPassword", p.PaymentPassword}, {"paymentIv", p.PaymentIV}} {
			if kv[1] == "" {
				p.Missing = append(p.Missing, kv[0])
			}
		}
		for id, v := range f.IdentifierV3Passwords {
			if v != "" {
				p.IdentifierV3Passwords[strings.ToUpper(id)] = v
			}
		}
		prefix := envVar(name, "IDENTIFIER_PASSWORD") + "_"
		for _, kv := range os.Environ() {
			k, v, _ := strings.Cut(kv, "=")
			if strings.HasPrefix(k, prefix) && v != "" {
				p.IdentifierV3Passwords[strings.ToUpper(k[len(prefix):])] = v
			}
		}
		out[name] = p
	}
	return out, nil
}

// preset returns the environment's ciphers, falling back to "other" like the
// original.
func (s *Service) preset(env string) (Preset, error) {
	all, err := s.ciphers()
	if err != nil {
		return Preset{}, err
	}
	if p, ok := all[env]; ok {
		return p, nil
	}
	return all["other"], nil
}

func (s *Service) missingPasswordErr(env string, p Preset) error {
	return userErr("No cipher password configured for %q (missing: %s). Fill in %s or set the TSEL_%s_* variables.",
		env, strings.Join(p.Missing, ", "), s.CiphersFile, strings.ToUpper(env))
}
