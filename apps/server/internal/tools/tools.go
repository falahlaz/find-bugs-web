// Package tools hosts the MyTelkomsel support tools (payment deeplink,
// hashsign, package ID, token decrypt), ported from the mytsel-tools repo.
//
// A tool declares its form fields and a Run function; the browser renders the
// form from the manifest, so adding a tool is one file here and an entry in
// All. Run never sees a field the user couldn't see: values are coerced to
// the declared types and fields hidden by ShowIf are dropped first.
package tools

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools/tselcrypto"
)

// Field types.
const (
	Text     = "text"
	Textarea = "textarea"
	Select   = "select"
	Tabs     = "tabs" // a select drawn as a segmented control
	Number   = "number"
	Checkbox = "checkbox"
)

// ToolOption is one choice of a select or tabs field.
type ToolOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ToolShowIf hides a field unless another field equals a value (or one of In).
type ToolShowIf struct {
	Field  string   `json:"field"`
	Equals string   `json:"equals,omitempty"`
	In     []string `json:"in,omitempty"`
}

// ToolField is one form input.
type ToolField struct {
	Name        string       `json:"name"`
	Label       string       `json:"label"`
	Type        string       `json:"type" enum:"text,textarea,select,tabs,number,checkbox"`
	Options     []ToolOption `json:"options,omitempty"`
	Default     string       `json:"default"`
	Placeholder string       `json:"placeholder"`
	Help        string       `json:"help"`
	Required    bool         `json:"required"`
	ShowIf      *ToolShowIf  `json:"showIf"`
	// Group lays consecutive fields with the same group side by side.
	Group string `json:"group"`
}

// ToolSummaryItem is one row of the result's key/value table.
type ToolSummaryItem struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret,omitempty" doc:"Hidden until revealed"`
}

// ToolOutput is a copyable result value.
type ToolOutput struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Kind  string `json:"kind" enum:"code,link"`
}

// ToolResult is what a tool run returns.
type ToolResult struct {
	Summary  []ToolSummaryItem `json:"summary"`
	Outputs  []ToolOutput      `json:"outputs"`
	Warnings []string          `json:"warnings"`
}

// Tool is one support tool.
type Tool struct {
	ID          string
	Name        string
	Description string
	Docs        string
	Fields      []ToolField
	Run         func(s *Service, in Input) (ToolResult, error)
}

// ToolInfo is a tool as the browser sees it (everything but Run).
type ToolInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Docs        string      `json:"docs"`
	Fields      []ToolField `json:"fields"`
}

// UserError is a problem the user can fix (bad input, missing config); its
// message is shown as-is.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(format string, args ...any) error { return &UserError{Msg: fmt.Sprintf(format, args...)} }

// InputError lists the fields that failed validation.
type InputError struct{ Problems []string }

func (e *InputError) Error() string { return strings.Join(e.Problems, " ") }

// ErrUnknownTool is returned by Run for an id that isn't registered.
var ErrUnknownTool = errors.New("unknown tool")

// Service runs the tools with the secrets from the server config.
type Service struct {
	// CiphersFile is the JSON cipher table (format of config/ciphers.example.json).
	CiphersFile string
	// PrivateKeyFile is the RSA key for hashsign and profilePlan.
	PrivateKeyFile string
	// Location interprets dates typed without a timezone.
	Location *time.Location
	Now      func() time.Time

	tools []Tool
}

// New registers every tool.
func New(ciphersFile, privateKeyFile string, loc *time.Location) *Service {
	if loc == nil {
		loc = time.Local
	}
	return &Service{
		CiphersFile: ciphersFile, PrivateKeyFile: privateKeyFile, Location: loc, Now: time.Now,
		tools: []Tool{paymentDeeplink, hashsignTool, packageID, tokenDecrypt},
	}
}

// List is the manifest, in sidebar order.
func (s *Service) List() []ToolInfo {
	out := make([]ToolInfo, 0, len(s.tools))
	for _, t := range s.tools {
		fields := make([]ToolField, len(t.Fields))
		for i, f := range t.Fields {
			if f.Type == "" {
				f.Type = Text
			}
			if f.Label == "" {
				f.Label = f.Name
			}
			fields[i] = f
		}
		out = append(out, ToolInfo{ID: t.ID, Name: t.Name, Description: t.Description, Docs: t.Docs, Fields: fields})
	}
	return out
}

// Run validates raw and runs tool id. Errors are *InputError (400),
// *UserError (422), ErrUnknownTool (404) or a bug.
func (s *Service) Run(id string, raw map[string]any) (ToolResult, error) {
	i := slices.IndexFunc(s.tools, func(t Tool) bool { return t.ID == id })
	if i < 0 {
		return ToolResult{}, ErrUnknownTool
	}
	t := s.tools[i]
	in, problems := Normalise(t.Fields, raw)
	if len(problems) > 0 {
		return ToolResult{}, &InputError{Problems: problems}
	}
	res, err := t.Run(s, in)
	if err != nil {
		return ToolResult{}, err
	}
	if res.Summary == nil {
		res.Summary = []ToolSummaryItem{}
	}
	if res.Outputs == nil {
		res.Outputs = []ToolOutput{}
	}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	return res, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) privateKey() (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(s.PrivateKeyFile)
	if err != nil {
		return nil, userErr("Private key not found at %s. Put the services' private.pem there or set TSEL_PRIVATE_KEY_PATH.", s.PrivateKeyFile)
	}
	k, err := tselcrypto.ParsePrivateKey(b)
	if err != nil {
		return nil, userErr("Private key at %s is unreadable: %v", s.PrivateKeyFile, err)
	}
	return k, nil
}

// Input is a tool's validated form values: string for text-like fields, bool
// for checkboxes, float64 or nil for numbers. Hidden fields are absent.
type Input map[string]any

// Str returns a text-like field, "" when hidden.
func (in Input) Str(name string) string {
	s, _ := in[name].(string)
	return s
}

// Num returns a number field and whether it was filled in.
func (in Input) Num(name string) (float64, bool) {
	f, ok := in[name].(float64)
	return f, ok
}

// Bool returns a checkbox field.
func (in Input) Bool(name string) bool {
	b, _ := in[name].(bool)
	return b
}

func fieldLabel(f ToolField) string {
	if f.Label != "" {
		return f.Label
	}
	return f.Name
}

// ActiveFields are the names of the fields whose ShowIf currently holds.
// Conditions are transitive: a field is hidden when the field it depends on
// is itself hidden, even if a stale value happens to match. The browser
// mirrors this so both sides agree on what the user can see.
func ActiveFields(fields []ToolField, values map[string]any) map[string]bool {
	byName := map[string]ToolField{}
	for _, f := range fields {
		byName[f.Name] = f
	}
	memo := map[string]bool{}
	visiting := map[string]bool{}
	var active func(f ToolField) bool
	active = func(f ToolField) bool {
		if v, ok := memo[f.Name]; ok {
			return v
		}
		if visiting[f.Name] {
			return true // a ShowIf cycle shouldn't hide everything
		}
		visiting[f.Name] = true
		res := true
		if f.ShowIf != nil {
			if parent, ok := byName[f.ShowIf.Field]; ok && !active(parent) {
				res = false
			} else {
				actual, _ := values[f.ShowIf.Field].(string)
				if f.ShowIf.In != nil {
					res = slices.Contains(f.ShowIf.In, actual)
				} else {
					res = actual == f.ShowIf.Equals
				}
			}
		}
		delete(visiting, f.Name)
		memo[f.Name] = res
		return res
	}
	out := map[string]bool{}
	for _, f := range fields {
		if active(f) {
			out[f.Name] = true
		}
	}
	return out
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

// Normalise coerces raw (a JSON object) into the declared types, drops hidden
// fields and reports anything required but missing.
func Normalise(fields []ToolField, raw map[string]any) (Input, []string) {
	values := map[string]any{}
	// Two passes: ShowIf may reference a field declared later.
	for _, f := range fields {
		v, ok := raw[f.Name]
		if !ok || v == nil {
			v = f.Default
		}
		if f.Type != Number && f.Type != Checkbox {
			v = strings.TrimSpace(toString(v))
		}
		values[f.Name] = v
	}
	active := ActiveFields(fields, values)

	in := Input{}
	var problems []string
	for _, f := range fields {
		if !active[f.Name] {
			continue
		}
		v := values[f.Name]
		label := fieldLabel(f)
		switch f.Type {
		case Checkbox:
			in[f.Name] = v == true || v == "true" || v == "on"
			continue
		case Number:
			s := strings.TrimSpace(toString(v))
			if s == "" {
				if f.Required {
					problems = append(problems, fmt.Sprintf("%q is required.", label))
					continue
				}
				in[f.Name] = nil
				continue
			}
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%q must be a number.", label))
				continue
			}
			in[f.Name] = n
			continue
		}
		s := v.(string)
		if (f.Type == Select || f.Type == Tabs) && s != "" &&
			!slices.ContainsFunc(f.Options, func(o ToolOption) bool { return o.Value == s }) {
			problems = append(problems, fmt.Sprintf("%q has an unsupported value.", label))
			continue
		}
		if f.Required && s == "" {
			problems = append(problems, fmt.Sprintf("%q is required.", label))
			continue
		}
		in[f.Name] = s
	}
	return in, problems
}
