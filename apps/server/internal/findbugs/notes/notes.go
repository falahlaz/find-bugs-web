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

// ErrorTypeKey normalizes an error type.
func ErrorTypeKey(errorType string) string {
	return spaces.ReplaceAllString(strings.ToLower(strings.TrimSpace(errorType)), " ")
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
