package immobilienscout24

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// encoding/xml quotes the bytes it refuses in its errors, such as a CSI
// (U+009B) or a byte that is not UTF-8. So every read that decodes a response,
// and a create that looks for the new id, escapes a decoding error too.
func TestMalformedResponsesAreEscapedInErrors(t *testing.T) {
	const bad = "\u009b\xff"
	ctx := context.Background()
	get := func(body string) *Client { return stubServer(t, http.StatusOK, nil, body) }
	_, rootErr := get(`<realestates:apartmentRent`+bad+`/>`).GetApartmentRent(ctx, "1")
	_, childErr := get(`<realestates:apartmentRent xmlns:realestates="`+realEstatesNamespace+`"><title`+bad+
		`>t</title></realestates:apartmentRent>`).GetApartmentRent(ctx, "1")
	_, attachmentErr := get(`<common:attachment`+bad+`/>`).GetAttachment(ctx, "1", "2")
	_, contactErr := get(`<common:realtorContactDetail`+bad+`/>`).GetContact(ctx, "1")
	_, publicationErr := get(`<common:publishObject`+bad+`/>`).GetPublication(ctx, "1_2")
	_, createErr := stubServer(t, http.StatusCreated, nil, `<common:messages`+bad+`/>`).CreateApartmentRent(ctx, minimalDocument())
	for _, c := range []struct {
		err  error
		want string
	}{
		{rootErr, `decoding real estate response: XML syntax error on line 1: invalid XML name: realestates:apartmentRent\u009b\xff`},
		{childErr, `decoding real estate response: XML syntax error on line 1: invalid XML name: title\u009b\xff`},
		{attachmentErr, `decoding attachment response: XML syntax error on line 1: invalid XML name: common:attachment\u009b\xff`},
		{contactErr, `decoding contact response: XML syntax error on line 1: invalid XML name: common:realtorContactDetail\u009b\xff`},
		{publicationErr, `decoding publication response: XML syntax error on line 1: invalid XML name: common:publishObject\u009b\xff`},
		{createErr, `response is not a <common:messages> document: XML syntax error on line 1: invalid XML name: common:messages\u009b\xff`},
	} {
		if c.err == nil || !strings.Contains(c.err.Error(), c.want) ||
			strings.Contains(c.err.Error(), "\u009b") || strings.Contains(c.err.Error(), "\xff") {
			t.Errorf("want an error with %s and no raw CSI or 0xff, got %q", c.want, c.err)
		}
	}
}

// roundTripperFunc answers a request, or fails it, without a server.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A transport error can quote the server, as a TLS error quotes the names in
// its certificate. It is escaped whatever verifier the platform uses, so the
// transport here fails every request with such a text itself.
func TestTransportErrorsAreEscaped(t *testing.T) {
	c := NewClient("https://api.test/restapi/api", "ck", "cs", "at", "ats", "test")
	c.httpClient.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("x509: certificate is valid for san\x1b[31m\u009b\xff.example, not api.test")
	})
	err := c.DeleteRealEstate(context.Background(), "1")
	want := `DELETE /offer/v1.0/user/me/realestate/1: x509: certificate is valid for san\x1b[31m\u009b\xff.example, not api.test`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want an error with %s, got %q", want, err)
	}
	for _, raw := range []string{"\x1b", "\u009b", "\xff"} {
		if strings.Contains(err.Error(), raw) {
			t.Errorf("the error contains a raw %q: %q", raw, err)
		}
	}
}
