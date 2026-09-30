package immobilienscout24

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// APIError is a non-2xx response. It never carries request headers, so it
// cannot leak the OAuth Authorization header. The texts that come from the
// response, the body and each message's code and text, are capped at
// maxErrorBodyBytes and made safe for a terminal; see errorText.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Messages   []Message
	// Body holds the start of the response body when it was not a
	// <common:messages> document.
	Body string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: HTTP %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	for _, m := range e.Messages {
		fmt.Fprintf(&b, "\n%s: %s", m.Code, strings.TrimSpace(m.Text))
	}
	if len(e.Messages) == 0 && e.Body != "" {
		fmt.Fprintf(&b, "\n%s", e.Body)
	}
	return b.String()
}

// Codes returns the message codes of the error response.
func (e *APIError) Codes() []string {
	codes := make([]string, 0, len(e.Messages))
	for _, m := range e.Messages {
		codes = append(codes, m.Code)
	}
	return codes
}

// Is reports whether the error is a documented not-found or conflict response,
// or the refusal to delete the default contact.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound && e.hasCode(codeResourceNotFound, codeCommonResourceNotFound)
	case ErrConflict:
		return e.StatusCode == http.StatusConflict && e.hasCode(codeRequestConflict)
	case ErrDefaultContact:
		return e.StatusCode == http.StatusPreconditionFailed && slices.ContainsFunc(e.Messages, func(m Message) bool {
			return m.Code == codeResourceValidation && defaultContactUndeletable.MatchString(m.Text)
		})
	}
	return false
}

func (e *APIError) hasCode(codes ...string) bool {
	for _, m := range e.Messages {
		if slices.Contains(codes, m.Code) {
			return true
		}
	}
	return false
}

func newAPIError(method, path string, status int, body []byte) *APIError {
	apiErr := &APIError{Method: method, Path: path, StatusCode: status}
	if msgs, err := parseMessages(body); err == nil && len(msgs) > 0 {
		for i := range msgs {
			msgs[i].Code, msgs[i].Text = errorText(msgs[i].Code), errorText(msgs[i].Text)
		}
		apiErr.Messages = msgs
		return apiErr
	}
	// Some errors (429, gateway errors) come back as plain text.
	apiErr.Body = errorText(strings.TrimSpace(string(body)))
	return apiErr
}

// errorText prepares a text that comes from a response for an error, which
// Terraform shows on the terminal: it cuts the text to maxErrorBodyBytes, at a
// character boundary, and escapes every control character but newline and
// tab, C0, DEL and C1, and every byte that is not UTF-8. Without the escaping,
// a response could move the cursor, clear the screen or change colours, and so
// make a failed apply look like a success.
func errorText(s string) string {
	truncated := len(s) > maxErrorBodyBytes
	if truncated {
		cut := maxErrorBodyBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r >= 0x80 && r <= 0x9f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	if truncated {
		b.WriteString("... (truncated)")
	}
	return b.String()
}
