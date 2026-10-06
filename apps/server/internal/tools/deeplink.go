package tools

import (
	"slices"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// paymentDeeplink builds the two share URL formats from
// #constructShareOfferURL in offers.service.js:
//
//	payment-method  {WEBUI_URL}/app/payment-method?link={encryptedLink}
//	package-detail  {WEBUI_URL}/app/package-details/{encryptedBID}[?campaignId=...]
var paymentDeeplink = Tool{
	ID:          "payment-deeplink",
	Name:        "Payment Deeplink",
	Description: "Build encrypted package-detail and payment-method share links for a BID.",
	Docs:        "Mirrors #constructShareOfferURL in offers.service.js.",
	Fields: []ToolField{
		{Name: "bid", Label: "BID", Required: true, Placeholder: "00093370", Help: "The business product ID to encrypt."},
		{Name: "environment", Label: "Environment", Type: Select, Default: "other", Options: environmentOptions,
			Help: "Picks the cipher passwords and the web UI host."},
		{Name: "linkType", Label: "Link type", Type: Select, Default: "both", Options: []ToolOption{
			{Value: "both", Label: "Both"},
			{Value: "payment-method", Label: "Payment method"},
			{Value: "package-detail", Label: "Package detail"},
		}},
		{Name: "encryption", Label: "Encryption", Type: Select, Default: "v1", Options: []ToolOption{
			{Value: "v1", Label: "v1 — AES-256-CBC"},
			{Value: "v2", Label: `v2 — AES-256-GCM ("v2::" prefix)`},
		}, Help: "The offers API now returns v2; v1 stays the default for older payment links."},
		{Name: "keyDerivation", Label: "v1 key derivation", Type: Select, Default: "legacy",
			ShowIf: &ToolShowIf{Field: "encryption", Equals: "v1"}, Options: derivationOptions,
			Help: "v1 links differ depending on the Node version the service runs. Pick the one matching the target service, or try both if a link is rejected."},
		{Name: "packagePassword", Label: "Package cipher override", Placeholder: "leave blank to use the environment preset",
			Help: "Overrides PACKAGE_CIPHER_PASSWORD. Required for v2 on the fallback environment."},
		{Name: "campaignId", Label: "Campaign ID", ShowIf: &ToolShowIf{Field: "linkType", In: withDetail},
			Help: "Optional. Appended to the package-detail URL only."},
		{Name: "campaignTrackingId", Label: "Campaign tracking ID", ShowIf: &ToolShowIf{Field: "linkType", In: withDetail}},
	},
	Run: runDeeplink,
}

var (
	withPayment = []string{"payment-method", "both"}
	withDetail  = []string{"package-detail", "both"}
)

var derivationOptions = []ToolOption{
	{Value: "legacy", Label: "Legacy EVP_BytesToKey — services on Node <= 21"},
	{Value: "scrypt", Label: "scrypt patch — services on Node >= 22"},
}

func withOverride(v string, override bool) string {
	if override {
		return v + "  (override)"
	}
	return v
}

func runDeeplink(s *Service, in Input) (ToolResult, error) {
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
	// An empty password would encrypt happily into a link that works nowhere.
	if pw == "" {
		return ToolResult{}, s.missingPasswordErr(env, p)
	}
	isV2 := in.Str("encryption") == "v2"
	d := tselcrypto.Derivation(in.Str("keyDerivation"))
	if d == "" {
		d = tselcrypto.Legacy
	}
	// encryptPackageIDWithGCM reads PACKAGE_CIPHER_PASSWORD with no fallback,
	// so the fallback preset (which means "unset") cannot satisfy it.
	if isV2 && env == "fallback" && override == "" {
		return ToolResult{}, userErr("v2 (GCM) has no fallback password in module-common-function. Choose another environment or set a package cipher override.")
	}

	bid := in.Str("bid")
	var encBID string
	if isV2 {
		encBID, err = tselcrypto.EncryptPackageIDWithGCM(bid, pw)
	} else {
		encBID, err = tselcrypto.EncryptPackageID(bid, pw, d)
	}
	if err != nil {
		return ToolResult{}, userErr("Encrypt failed: %v", err)
	}

	linkType := in.Str("linkType")
	payment, detail := slices.Contains(withPayment, linkType), slices.Contains(withDetail, linkType)
	if payment && p.PaymentPassword == "" {
		return ToolResult{}, userErr("No payment cipher configured for %q. Fill in %s.", env, s.CiphersFile)
	}

	var res ToolResult
	if payment {
		// offers.service.js -> #__paymentMethodLinkFormat
		link, err := tselcrypto.EncryptDeeplinkPayment("PAC|"+encBID, p.PaymentPassword, p.PaymentIV)
		if err != nil {
			return ToolResult{}, userErr("Payment cipher for %q is invalid: %v (it needs a 32-byte password and a 16-byte IV).", env, err)
		}
		res.Outputs = append(res.Outputs, ToolOutput{Label: "Payment method link", Value: p.WebUIURL + "/app/payment-method?link=" + link, Kind: "link"})
	}
	if detail {
		// offers.service.js -> getPackageDetailShareURL
		url := p.WebUIURL + "/app/package-details/" + encBID
		if c, t := in.Str("campaignId"), in.Str("campaignTrackingId"); c != "" || t != "" {
			url += "?campaignId=" + c + "&campaignTrackingId=" + t
		}
		res.Outputs = append(res.Outputs, ToolOutput{Label: "Package detail link", Value: url, Kind: "link"})
	}

	enc := "v2 (AES-256-GCM)"
	if !isV2 {
		enc = "v1 (AES-256-CBC, " + string(d) + ")"
	}
	res.Summary = []ToolSummaryItem{
		{Label: "Environment", Value: p.Label},
		{Label: "BID", Value: bid},
		{Label: "Encryption", Value: enc},
		{Label: "Encrypted BID", Value: encBID},
		{Label: "Package cipher", Value: withOverride(pw, override != ""), Secret: true},
	}
	if payment {
		res.Summary = append(res.Summary, ToolSummaryItem{Label: "Payment cipher", Value: p.PaymentPassword + " / iv " + p.PaymentIV, Secret: true})
	}
	if !isV2 {
		if d == tselcrypto.Legacy {
			res.Warnings = append(res.Warnings, "Legacy derivation: valid for services running Node 21 or older. If the link is rejected, retry with the scrypt option.")
		} else {
			res.Warnings = append(res.Warnings, "scrypt derivation: valid for services running Node 22 or newer, where module-common-function patches createCipher.")
		}
	}
	return res, nil
}
