// Package parser turns QA input (a bare transaction ID or a pasted curl
// command) into a transaction ID. Ported from find-bugs-bot parser/curl_parser.py
// and the validation in bot/handler.py.
package parser

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var txnRe = regexp.MustCompile(`^[A-Za-z0-9\-_.]{1,128}$`)

// Input kinds stored with each job.
const (
	KindTransactionID = "transaction_id"
	KindCurl          = "curl"
)

// Error is a user-facing validation error.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func userErr(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

// IsUserError reports whether err is a validation error safe to show to users.
func IsUserError(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

// SprintHeader is the curl header naming the app sprint build that sent the request.
const SprintHeader = "X-SPRINT-IDENTIFIER"

// ValidTransactionID reports whether s matches the bot's transaction ID rule.
func ValidTransactionID(s string) bool { return txnRe.MatchString(s) }

// Result is the outcome of parsing user input.
type Result struct {
	TransactionID string
	Kind          string
	// SprintIdentifier is the curl's SprintHeader value, if present and valid.
	SprintIdentifier string
}

// Parse accepts a transaction ID or a curl command. header is the name of the
// header carrying the transaction ID (TRANSACTION_ID_HEADER).
func Parse(input, header string) (Result, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return Result{}, userErr("Input kosong. Paste transaction ID atau curl command.")
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "curl") || (strings.Contains(lower, "-h ") && strings.Contains(lower, "http")) {
		c, err := ParseCurl(text, header)
		if err != nil {
			return Result{}, err
		}
		r := Result{TransactionID: c.TransactionID, Kind: KindCurl}
		if s := c.Headers[strings.ToLower(SprintHeader)]; ValidTransactionID(s) {
			r.SprintIdentifier = s
		}
		return r, nil
	}
	if !ValidTransactionID(text) {
		return Result{}, userErr("Itu bukan transaction ID atau curl command yang valid. Paste nilai %s atau curl lengkap.", header)
	}
	return Result{TransactionID: text, Kind: KindTransactionID}, nil
}

// Curl is a parsed curl command.
type Curl struct {
	TransactionID string
	Method        string
	URL           string
	Headers       map[string]string // lower-cased names
	Body          string
}

// ParseCurl extracts the request and the transaction ID header from a curl command.
func ParseCurl(raw, header string) (Curl, error) {
	normalized := strings.TrimSpace(raw)
	normalized = strings.ReplaceAll(normalized, "\\\r\n", " ")
	normalized = strings.ReplaceAll(normalized, "\\\n", " ")
	if !strings.HasPrefix(strings.ToLower(normalized), "curl") {
		return Curl{}, userErr("Itu tidak terlihat seperti curl command yang valid. Paste curl lengkap.")
	}
	tokens, err := shellSplit(normalized)
	if err != nil {
		return Curl{}, userErr("Curl command tidak bisa di-parse: %v", err)
	}

	c := Curl{Headers: map[string]string{}}
	hasBody := false
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		next := i+1 < len(tokens)
		switch {
		case (tok == "-X" || tok == "--request") && next:
			c.Method = strings.ToUpper(tokens[i+1])
			i++
		case (tok == "-H" || tok == "--header") && next:
			if k, v, ok := strings.Cut(tokens[i+1], ":"); ok {
				c.Headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
			i++
		case isDataFlag(tok) && next:
			c.Body, hasBody = tokens[i+1], true
			if c.Method == "" {
				c.Method = "POST"
			}
			i++
		case tok == "--url" && next:
			c.URL = tokens[i+1]
			i++
		case !strings.HasPrefix(tok, "-") && c.URL == "":
			c.URL = tok
		}
	}
	if c.URL == "" {
		return Curl{}, userErr("URL tidak ditemukan di curl command.")
	}
	if c.Method == "" {
		c.Method = "GET"
		if hasBody {
			c.Method = "POST"
		}
	}
	txn, ok := c.Headers[strings.ToLower(header)]
	if !ok {
		return Curl{}, userErr("Header `%s` tidak ditemukan di curl. Pastikan header itu ikut di-copy.", header)
	}
	if !ValidTransactionID(txn) {
		return Curl{}, userErr("Transaction ID berisi karakter tidak valid. Hanya huruf, angka, '-', '_' dan '.' yang boleh.")
	}
	c.TransactionID = txn
	return c, nil
}

func isDataFlag(t string) bool {
	switch t {
	case "-d", "--data", "--data-raw", "--data-binary", "--data-urlencode", "--data-ascii":
		return true
	}
	return false
}

// shellSplit splits s like POSIX sh (Python shlex.split): whitespace separates
// words, single quotes are literal, double quotes allow \" \\ \$ \` escapes.
func shellSplit(s string) ([]string, error) {
	var (
		out     []string
		cur     strings.Builder
		inWord  bool
		r       = []rune(s)
		isSpace = func(c rune) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case isSpace(c):
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\'':
			inWord = true
			j := i + 1
			for j < len(r) && r[j] != '\'' {
				cur.WriteRune(r[j])
				j++
			}
			if j >= len(r) {
				return nil, errors.New("no closing quotation")
			}
			i = j
		case c == '"':
			inWord = true
			j := i + 1
			for ; j < len(r) && r[j] != '"'; j++ {
				if r[j] == '\\' && j+1 < len(r) && strings.ContainsRune("\"\\$`\n", r[j+1]) {
					j++
				}
				cur.WriteRune(r[j])
			}
			if j >= len(r) {
				return nil, errors.New("no closing quotation")
			}
			i = j
		case c == '\\':
			inWord = true
			if i+1 >= len(r) {
				return nil, errors.New("no escaped character")
			}
			i++
			cur.WriteRune(r[i])
		default:
			inWord = true
			cur.WriteRune(c)
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}
