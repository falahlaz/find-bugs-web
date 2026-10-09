// Package notes matches engineer notes to diagnoses: a note is keyed by the
// failed component and error type, normalized so the same error matches
// across environments, hosts and request IDs.
package notes

import (
	"regexp"
	"strings"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

var (
	// envSegment is a path segment naming an environment, such as
	// preprod-web or dev, which differs per deployment of the same endpoint.
	envSegment = regexp.MustCompile(`^(prod|preprod|pre-prod|dev|devel|staging|stg|sit|uat|qa|test)(-[a-z0-9]+)?$`)
	// idSegment is a path segment that varies per request: a number, a
	// UUID, a long hex string or a token mixing letters and many digits.
	idSegment = regexp.MustCompile(`^(\d+|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{16,}|[a-z]*\d[a-z0-9]*\d{5,}[a-z0-9]*)$`)
	spaces    = regexp.MustCompile(`\s+`)
)

// ComponentKey normalizes a failed component. A URL or path keeps only its
// path, without environment or ID segments; anything else is lowercased.
func ComponentKey(component string) string {
	c := strings.ToLower(strings.TrimSpace(component))
	if c == "" {
		return ""
	}
	if i := strings.Index(c, "://"); i >= 0 {
		rest := c[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			c = rest[j:]
		} else {
			c = "/"
		}
	}
	if !strings.HasPrefix(c, "/") || strings.ContainsAny(c, " \t") {
		return spaces.ReplaceAllString(c, " ")
	}
	if i := strings.IndexAny(c, "?#"); i >= 0 {
		c = c[:i]
	}
	var segs []string
	for _, s := range strings.Split(c, "/") {
		switch {
		case s == "" || envSegment.MatchString(s):
		case idSegment.MatchString(s):
			segs = append(segs, "{id}")
		default:
			segs = append(segs, s)
		}
	}
	return "/" + strings.Join(segs, "/")
}

var (
	htmlTag = regexp.MustCompile(`<[^>]*>`)
	// appCode is an application error code such as SYS-UXP-0021 or
	// 30RV-0006: dash-joined parts with a letter, ending in 3+ digits.
	appCode = regexp.MustCompile(`(?:^|[^a-z0-9-])([a-z0-9]*[a-z][a-z0-9]*(?:-[a-z0-9]+)*-\d{3,})(?:$|[^a-z0-9-])`)
	// httpStatus is a leading HTTP status and its standard reason phrase,
	// as in "HTTP 400 Bad Request" (matched after punctuation is removed).
	httpStatus = regexp.MustCompile(`^(?:http )?([1-5]\d\d)(?: (?:bad request|unauthorized|forbidden|not found|method not allowed|request timeout|conflict|` +
		`unprocessable entity|too many requests|internal server error|not implemented|bad gateway|service unavailable|gateway timeout))?(?: |$)`)
	// labeledCode is a code the error message names, as in "(error code 3002)".
	labeledCode = regexp.MustCompile(`\b(?:status |error )?code (\w+)`)
	// qualifiedClass is an exception class with its package, as in
	// java.lang.NullPointerException.
	qualifiedClass = regexp.MustCompile(`\b(?:[a-z_][a-z0-9_]*\.)+([a-z][a-z0-9_]*(?:exception|error))\b`)
	nonWord        = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// ErrorTypeKey normalizes an error type to the code inside it, since the
// model writes the same error in different ways: an application code
// ("SYS-UXP-0021: Internal Application Error…" → "sys-uxp-0021"), else a
// leading HTTP status without its reason phrase, followed by the code or
// message after it ("HTTP 400 Bad Request" → "400", "400 - … (error code
// 3002)" → "400 3002"), else the words without punctuation or exception
// package ("cache_miss" → "cache miss"). A status alone stays apart from a
// status with a message, since one endpoint fails with a status for
// different reasons.
func ErrorTypeKey(errorType string) string {
	s := strings.ToLower(htmlTag.ReplaceAllString(errorType, " "))
	if m := appCode.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	s = qualifiedClass.ReplaceAllString(s, "$1")
	s = strings.TrimSpace(nonWord.ReplaceAllString(s, " "))
	if m := httpStatus.FindStringSubmatch(s); m != nil {
		rest := s[len(m[0]):]
		if c := labeledCode.FindStringSubmatch(rest); c != nil {
			rest = c[1]
		}
		return strings.TrimSpace(m[1] + " " + rest)
	}
	return s
}

// Keys returns the note keys for a diagnosis; anyType asks for the key of a
// note covering every error type of the component. ok is false when the
// diagnosis names no component to match on.
func Keys(component, errorType string, anyType bool) (componentKey, errorTypeKey string, ok bool) {
	componentKey = ComponentKey(component)
	if anyType {
		errorTypeKey = store.AnyErrorType
	} else {
		errorTypeKey = ErrorTypeKey(errorType)
	}
	return componentKey, errorTypeKey, componentKey != "" && errorTypeKey != ""
}
