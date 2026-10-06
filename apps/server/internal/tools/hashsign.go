package tools

import (
	"crypto/rsa"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// The hashsign header is an RSA-OAEP ciphertext (base64) of
//
//	msisdn | msisdnbeneficiary | paymentMethod | msisdninitiator | os | appVersion | timestamp
//
// validateHashSign (server/helpers/paymentHelper.js) reads indexes 1/2/3/6
// and rejects timestamps older than tsHashExpiration, but only for channelid
// UX on app version 5.12.0 or newer.

var hashsignFields = []string{"msisdn", "msisdnbeneficiary", "paymentMethod", "msisdninitiator", "os", "appVersion", "timestamp"}

var hashsignLabels = map[string]string{
	"msisdn": "MSISDN", "msisdnbeneficiary": "Beneficiary MSISDN", "paymentMethod": "Payment method",
	"msisdninitiator": "Initiator MSISDN", "os": "OS", "appVersion": "App version", "timestamp": "Timestamp",
}

var hashsignPlaceholders = map[string]string{
	"msisdn": "628139853636", "msisdnbeneficiary": "6282234508413", "paymentMethod": "OVO",
	"msisdninitiator": "62888888888", "os": "android", "appVersion": "9.4.0", "timestamp": "blank = now",
}

const (
	timestampIndex = 6
	// expiryMinutes matches tsHashExpiration.
	expiryMinutes = 5
)

var hashsignTool = Tool{
	ID:          "hashsign",
	Name:        "Payment Hashsign",
	Description: "Decrypt, re-encrypt, or refresh the timestamp on a payment hashsign header.",
	Docs:        "Validated by validateHashSign in server/helpers/paymentHelper.js.",
	Fields:      hashsignFormFields(),
	Run:         runHashsign,
}

func hashsignFormFields() []ToolField {
	fields := []ToolField{
		{Name: "action", Label: "What do you want to do?", Type: Tabs, Default: "bump", Options: []ToolOption{
			{Value: "bump", Label: "Refresh timestamp"},
			{Value: "decrypt", Label: "Inspect"},
			{Value: "encrypt", Label: "Create new"},
		}},
		{Name: "hashsign", Label: "hashsign", Type: Textarea, Required: true,
			ShowIf:      &ToolShowIf{Field: "action", In: []string{"decrypt", "bump"}},
			Placeholder: "Paste the base64 hashsign header from the request",
			Help:        "Copy it from the request headers in Charles, the browser dev tools, or the app log."},
		{Name: "bumpMode", Label: "New timestamp", Type: Select, Default: "now", ShowIf: &ToolShowIf{Field: "action", Equals: "bump"}, Options: []ToolOption{
			{Value: "now", Label: "Now — makes the hashsign valid again"},
			{Value: "offset", Label: "Shift the existing timestamp"},
			{Value: "absolute", Label: "A specific date and time"},
		}},
		{Name: "offsetDays", Label: "Days", Type: Number, Placeholder: "0", Group: "offset", ShowIf: &ToolShowIf{Field: "bumpMode", Equals: "offset"}},
		{Name: "offsetMinutes", Label: "Minutes", Type: Number, Placeholder: "0", Group: "offset", ShowIf: &ToolShowIf{Field: "bumpMode", Equals: "offset"}},
		{Name: "targetTime", Label: "Date and time", Required: true, ShowIf: &ToolShowIf{Field: "bumpMode", Equals: "absolute"},
			Placeholder: "2026-08-14T14:00:00+07:00",
			Help:        "An ISO date (without a timezone it's read as WIB) or a raw unix timestamp."},
		{Name: "inputMode", Label: "Build it from", Type: Select, Default: "fields", ShowIf: &ToolShowIf{Field: "action", Equals: "encrypt"}, Options: []ToolOption{
			{Value: "fields", Label: "Separate fields"},
			{Value: "raw", Label: "One pipe-delimited string"},
		}},
		{Name: "plaintext", Label: "Plaintext", Type: Textarea, Required: true, ShowIf: &ToolShowIf{Field: "inputMode", Equals: "raw"},
			Placeholder: "628...|6282...|OVO|6288...|android|9.4.0|1786691964",
			Help:        "Order: " + strings.Join(hashsignFields, " | ")},
	}
	for _, name := range hashsignFields {
		var help []string
		switch name {
		case "msisdnbeneficiary", "paymentMethod", "msisdninitiator":
			help = append(help, "Checked against the request body.")
		case "timestamp":
			help = append(help, "Leave blank to stamp the current time.")
		}
		fields = append(fields, ToolField{
			Name: "field_" + name, Label: hashsignLabels[name], Group: "hashsignFields",
			ShowIf: &ToolShowIf{Field: "inputMode", Equals: "fields"}, Placeholder: hashsignPlaceholders[name],
			Help: strings.Join(help, " "),
		})
	}
	return fields
}

func hashsignDecrypt(v string, key *rsa.PrivateKey) (string, error) {
	pt, err := tselcrypto.RSADecrypt(v, key)
	if err != nil {
		return "", userErr("Decrypt failed: %v. Wrong key, corrupt input, or not an OAEP ciphertext.", err)
	}
	return pt, nil
}

func (s *Service) describeTimestamp(v string) (text string, expired bool) {
	secs, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsInf(secs, 0) {
		return v + " (not a number)", false
	}
	t := time.UnixMilli(int64(secs * 1000))
	age := int(math.Round(float64(s.now().Sub(t)) / float64(time.Minute)))
	ageText := fmt.Sprintf("%d min old", age)
	if age < 0 {
		ageText = fmt.Sprintf("%d min in the future", -age)
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z") + " (" + ageText + ")", age > expiryMinutes
}

func fieldSummary(parts []string) []ToolSummaryItem {
	out := make([]ToolSummaryItem, len(hashsignFields))
	for i, name := range hashsignFields {
		v := ""
		if i < len(parts) {
			v = parts[i]
		}
		out[i] = ToolSummaryItem{Label: fmt.Sprintf("[%d] %s", i, name), Value: v}
	}
	return out
}

var unixDigits = regexp.MustCompile(`^\d+$`)

var dateLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

func (s *Service) parseDate(v string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, true
	}
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, v, s.Location); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func formatSeconds(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (s *Service) resolveTimestamp(current string, in Input) (string, error) {
	switch in.Str("bumpMode") {
	case "absolute":
		target := in.Str("targetTime")
		if unixDigits.MatchString(target) {
			if n, err := strconv.ParseInt(target, 10, 64); err == nil {
				return strconv.FormatInt(n, 10), nil
			}
		}
		t, ok := s.parseDate(target)
		if !ok {
			return "", userErr("Cannot parse the date %q.", target)
		}
		return strconv.FormatInt(t.Unix(), 10), nil
	case "offset":
		secs, err := strconv.ParseFloat(strings.TrimSpace(current), 64)
		if err != nil {
			secs = float64(s.now().Unix())
		}
		days, _ := in.Num("offsetDays")
		mins, _ := in.Num("offsetMinutes")
		if days == 0 && mins == 0 {
			return "", userErr(`Set a day or minute offset, or switch to "now".`)
		}
		return formatSeconds(secs + days*86400 + mins*60), nil
	}
	return strconv.FormatInt(s.now().Unix(), 10), nil
}

// buildResult re-encrypts and verifies with a decrypt roundtrip, as the CLI did.
func (s *Service) buildResult(plaintext string, key *rsa.PrivateKey, extra ...ToolSummaryItem) (ToolResult, error) {
	parts := strings.Split(plaintext, "|")
	hs, err := tselcrypto.RSAEncrypt(plaintext, key)
	if err != nil {
		return ToolResult{}, userErr("Encrypt failed: %v (the plaintext may be too long for the key).", err)
	}
	back, err := tselcrypto.RSADecrypt(hs, key)
	ts, expired := s.describeTimestamp(at(parts, timestampIndex))

	var warnings []string
	if err != nil || back != plaintext {
		warnings = append(warnings, "Roundtrip verification FAILED — do not use this hashsign.")
	}
	if expired {
		warnings = append(warnings, fmt.Sprintf("Timestamp is older than %d minutes and will likely be rejected.", expiryMinutes))
	}
	summary := append(append(extra, fieldSummary(parts)...), ToolSummaryItem{Label: "timestamp", Value: ts})
	return ToolResult{
		Summary:  summary,
		Outputs:  []ToolOutput{{Label: "Plaintext", Value: plaintext, Kind: "code"}, {Label: "hashsign", Value: hs, Kind: "code"}},
		Warnings: warnings,
	}, nil
}

func at(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

func runHashsign(s *Service, in Input) (ToolResult, error) {
	key, err := s.privateKey()
	if err != nil {
		return ToolResult{}, err
	}
	switch in.Str("action") {
	case "decrypt":
		pt, err := hashsignDecrypt(in.Str("hashsign"), key)
		if err != nil {
			return ToolResult{}, err
		}
		parts := strings.Split(pt, "|")
		ts, expired := s.describeTimestamp(at(parts, timestampIndex))
		res := ToolResult{
			Summary: append(fieldSummary(parts), ToolSummaryItem{Label: "timestamp", Value: ts}),
			Outputs: []ToolOutput{{Label: "Plaintext", Value: pt, Kind: "code"}},
		}
		if expired {
			res.Warnings = []string{fmt.Sprintf("Timestamp is older than %d minutes and will likely be rejected.", expiryMinutes)}
		}
		return res, nil
	case "bump":
		pt, err := hashsignDecrypt(in.Str("hashsign"), key)
		if err != nil {
			return ToolResult{}, err
		}
		parts := strings.Split(pt, "|")
		for len(parts) <= timestampIndex {
			parts = append(parts, "")
		}
		if parts[timestampIndex], err = s.resolveTimestamp(parts[timestampIndex], in); err != nil {
			return ToolResult{}, err
		}
		return s.buildResult(strings.Join(parts, "|"), key, ToolSummaryItem{Label: "Was", Value: pt})
	}

	if in.Str("inputMode") == "raw" {
		return s.buildResult(in.Str("plaintext"), key)
	}
	parts := make([]string, len(hashsignFields))
	for i, name := range hashsignFields {
		parts[i] = in.Str("field_" + name)
	}
	if parts[timestampIndex] == "" {
		parts[timestampIndex] = strconv.FormatInt(s.now().Unix(), 10)
	}
	return s.buildResult(strings.Join(parts, "|"), key)
}
