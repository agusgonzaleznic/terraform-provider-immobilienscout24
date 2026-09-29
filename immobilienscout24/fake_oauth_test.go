package immobilienscout24

// The OAuth 1.0a check of the fake API in fake_api_test.go.

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // OAuth 1.0a HMAC-SHA1 is what the API uses.
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// verifyOAuth checks an RFC 5849 HMAC-SHA1 signature against the test
// credentials. XML bodies are not part of the signature base string.
func verifyOAuth(r *http.Request) error {
	header, ok := strings.CutPrefix(r.Header.Get("Authorization"), "OAuth ")
	if !ok {
		return fmt.Errorf("no OAuth Authorization header")
	}
	params := map[string]string{}
	for _, part := range strings.Split(header, ",") {
		k, v, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return fmt.Errorf("malformed OAuth parameter %q", part)
		}
		unquoted, err := url.PathUnescape(strings.Trim(v, `"`))
		if err != nil {
			return err
		}
		params[k] = unquoted
	}
	for k, want := range map[string]string{
		"oauth_consumer_key":     fakeConsumerKey,
		"oauth_token":            fakeAccessToken,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_version":          "1.0",
	} {
		if params[k] != want {
			return fmt.Errorf("%s = %q, want %q", k, params[k], want)
		}
	}
	signature := params["oauth_signature"]
	var pairs []string
	for k, v := range params {
		if k != "oauth_signature" && k != "realm" {
			pairs = append(pairs, oauthEncode(k)+"="+oauthEncode(v))
		}
	}
	for k, vs := range r.URL.Query() {
		for _, v := range vs {
			pairs = append(pairs, oauthEncode(k)+"="+oauthEncode(v))
		}
	}
	sort.Strings(pairs)
	baseURL := "http://" + strings.ToLower(r.Host) + r.URL.EscapedPath()
	base := r.Method + "&" + oauthEncode(baseURL) + "&" + oauthEncode(strings.Join(pairs, "&"))
	mac := hmac.New(sha1.New, []byte(oauthEncode(fakeConsumerSecret)+"&"+oauthEncode(fakeAccessTokenSecret)))
	mac.Write([]byte(base))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); !hmac.Equal([]byte(signature), []byte(want)) {
		return fmt.Errorf("OAuth signature does not verify")
	}
	return nil
}

func oauthEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
