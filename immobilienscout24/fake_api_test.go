package immobilienscout24

// A stateful fake of the ImmobilienScout24 realestate resource. It is strict
// where the documentation is explicit, so that the acceptance tests double as
// a contract check:
//
//   - paths, methods and response bodies as documented (bodies verbatim apart
//     from the id, which the fake assigns);
//   - Accept on every request and Content-Type on writes, as documented;
//   - an OAuth 1.0a HMAC-SHA1 Authorization header whose signature verifies
//     against the test credentials;
//   - a request root of realestates:apartmentRent in the documented namespace,
//     unqualified children, and child order and required elements checked
//     against the live XSD fixture (see xsd_test.go), not a hand-written list.
//
// Behaviour the documentation does not pin down is simulated and marked
// "simulated" below.

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // OAuth 1.0a HMAC-SHA1 is what the API uses.
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	fakeConsumerKey       = "test-consumer-key"
	fakeConsumerSecret    = "test-consumer-secret"
	fakeAccessToken       = "test-access-token"
	fakeAccessTokenSecret = "test-access-token-secret"

	fakeCollectionPath = "/restapi/api/offer/v1.0/user/me/realestate/"
)

type recordedRequest struct {
	Method string
	Path   string
	Body   string
}

// xnode is a generic XML element, enough to store and re-serialise objects.
type xnode struct {
	Name     string
	Attrs    []xml.Attr
	Text     string
	Children []*xnode
}

func (n *xnode) child(name string) *xnode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

type fakeAPI struct {
	t      testing.TB
	server *httptest.Server

	topOrder     []xsdElement
	addressOrder []xsdElement

	mu       sync.Mutex
	nextID   int
	objects  map[string]*xnode
	requests []recordedRequest
	// created and deletedOutOfBand record object history for CheckDestroy.
	created          []string
	deletedOutOfBand map[string]bool
	// lowercaseAddress makes GET return street and city in lower case, as the
	// documented Retrieve example does.
	lowercaseAddress bool
}

func newFakeAPI(t testing.TB) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		t:                t,
		nextID:           315000001,
		objects:          map[string]*xnode{},
		deletedOutOfBand: map[string]bool{},
		topOrder:         mustElements(t, realEstatesNamespace, "ApartmentRent"),
		addressOrder:     mustElements(t, commonNamespace, "Wgs84Address"),
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

// BaseURL is the value for the provider's base_url.
func (f *fakeAPI) BaseURL() string { return f.server.URL + "/restapi/api" }

func (f *fakeAPI) Requests(method string) []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedRequest
	for _, r := range f.requests {
		if method == "" || r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeAPI) Object(id string) (*xnode, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[id]
	return o, ok
}

func (f *fakeAPI) IDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id := range f.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// History returns the ids of every object created through the API, and the
// set of those that were deleted out of band.
func (f *fakeAPI) History() ([]string, map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	oob := map[string]bool{}
	for id := range f.deletedOutOfBand {
		oob[id] = true
	}
	return append([]string(nil), f.created...), oob
}

// DeleteOutOfBand removes an object as if someone deleted it on the website.
func (f *fakeAPI) DeleteOutOfBand(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, id)
	f.deletedOutOfBand[id] = true
}

// SetOutOfBand changes a top-level element as if edited on the website.
func (f *fakeAPI) SetOutOfBand(id, element, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.objects[id].child(element); c != nil {
		c.Text = value
	}
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
	f.mu.Unlock()

	if err := verifyOAuth(r); err != nil {
		f.t.Logf("fake API: %s %s rejected: %v", r.Method, r.URL.Path, err)
		writeMessages(w, http.StatusUnauthorized, "ERROR_COMMON_AUTHENTICATION_REQUIRED", "Authentication is required. "+err.Error())
		return
	}
	// Basic Principles: an unsupported Accept type yields 404, not 406.
	if r.Header.Get("Accept") != mediaTypeXML {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != mediaTypeXML {
			writeMessages(w, http.StatusUnsupportedMediaType, "ERROR_COMMON_MEDIA_TYPE_UNSUPPORTED", "Unsupported media type.")
			return
		}
	}

	id, isItem := strings.CutPrefix(r.URL.Path, fakeCollectionPath)
	switch {
	case r.URL.Path == fakeCollectionPath && r.Method == http.MethodPost:
		f.create(w, body)
	case isItem && id != "" && !strings.Contains(id, "/"):
		f.item(w, r.Method, id, body)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

func (f *fakeAPI) create(w http.ResponseWriter, body []byte) {
	obj, err := f.validate(body)
	if err != nil {
		writeMessages(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", err.Error())
		return
	}
	f.mu.Lock()
	id := strconv.Itoa(f.nextID)
	f.nextID++
	f.created = append(f.created, id)
	f.store(id, obj)
	f.mu.Unlock()

	w.Header().Set("Location", f.BaseURL()+"/offer/v1.0/user/me/realestate/"+id)
	// Verbatim from the Insert a Real Estate page, with the fake's id.
	writeRaw(w, http.StatusCreated, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"
    xmlns:xlink="http://www.w3.org/1999/xlink">
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [REALESTATE] with id [`+id+`] has been created.</message>
        <id>`+id+`</id>
    </message>
</common:messages>`)
}

func (f *fakeAPI) item(w http.ResponseWriter, method, id string, body []byte) {
	f.mu.Lock()
	obj, exists := f.objects[id]
	f.mu.Unlock()
	if !exists {
		writeMessages(w, http.StatusNotFound, "ERROR_RESOURCE_NOT_FOUND", "Resource [realestate] with id ["+id+"] not found.")
		return
	}

	switch method {
	case http.MethodGet:
		writeRaw(w, http.StatusOK, f.render(id, obj))
	case http.MethodPut:
		updated, err := f.validate(body)
		if err != nil {
			writeMessages(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", err.Error())
			return
		}
		f.mu.Lock()
		f.store(id, updated)
		f.mu.Unlock()
		// Verbatim from the Update a Real Estate page.
		writeRaw(w, http.StatusOK, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"
    xmlns:xlink="http://www.w3.org/1999/xlink">
    <message>
        <messageCode>MESSAGE_RESOURCE_UPDATED</messageCode>
        <message>Resource [NAME] with id [ID] has been updated. </message>
    </message>
</common:messages>`)
	case http.MethodDelete:
		f.mu.Lock()
		delete(f.objects, id)
		f.mu.Unlock()
		// Verbatim from the Delete a Real Estate page, with the fake's id.
		writeRaw(w, http.StatusOK, `<?xml version="1.0" encoding="UTF-8"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
<message>
<messageCode>MESSAGE_RESOURCE_DELETED</messageCode>
<message>Resource [realestate] with id [`+id+`] has been deleted.</message>
<id>`+id+`</id>
</message>
</common:messages>`)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

// store saves an object and applies the documented server-side defaults. The
// caller holds f.mu.
func (f *fakeAPI) store(id string, obj *xnode) {
	// Insert page: without externalId "we'll set the scout object id
	// automatically as externalId".
	if obj.child("externalId") == nil {
		obj.Children = append([]*xnode{{Name: "externalId", Text: id}}, obj.Children...)
	}
	// The sandbox fills these in when a request leaves them out (observed
	// 2026-09-29); GET returns them like any other field.
	for _, d := range sandboxDefaults {
		if obj.child(d[0]) == nil {
			obj.Children = append(obj.Children, &xnode{Name: d[0], Text: d[1]})
		}
	}
	// The sandbox geocodes an address sent without coordinates.
	if a := obj.child("address"); a != nil && a.child("wgs84Coordinate") == nil {
		a.Children = append(a.Children, &xnode{Name: "wgs84Coordinate", Children: []*xnode{
			{Name: "latitude", Text: "52.52534"}, {Name: "longitude", Text: "13.36666"},
		}})
	}
	if f.lowercaseAddress {
		if a := obj.child("address"); a != nil {
			for _, name := range []string{"street", "city"} {
				if c := a.child(name); c != nil {
					c.Text = strings.ToLower(c.Text)
				}
			}
		}
	}
	f.objects[id] = obj
}

// validate parses a request body and checks it against the XSD fixture.
func (f *fakeAPI) validate(body []byte) (*xnode, error) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var root *xnode
	var stack []*xnode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed XML: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if tok.Name.Space != realEstatesNamespace || tok.Name.Local != "apartmentRent" {
					return nil, fmt.Errorf("root element is {%s}%s, want {%s}apartmentRent", tok.Name.Space, tok.Name.Local, realEstatesNamespace)
				}
			} else if tok.Name.Space != "" {
				return nil, fmt.Errorf("element <%s> is in namespace %q, the XSD declares unqualified elements", tok.Name.Local, tok.Name.Space)
			}
			n := &xnode{Name: tok.Name.Local}
			for _, a := range tok.Attr {
				if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
					n.Attrs = append(n.Attrs, a)
				}
			}
			if len(stack) == 0 {
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(tok)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty body")
	}
	if err := checkOrder("apartmentRent", childNames(root), f.topOrder); err != nil {
		return nil, err
	}
	if a := root.child("address"); a != nil {
		if err := checkOrder("address", childNames(a), f.addressOrder); err != nil {
			return nil, err
		}
	}
	if c := root.child("courtage"); c != nil && c.child("hasCourtage") == nil {
		return nil, fmt.Errorf("courtage: required element <hasCourtage> is missing")
	}
	for _, c := range root.Children {
		c.Text = strings.TrimSpace(c.Text)
	}
	return root, nil
}

func childNames(n *xnode) []string {
	var names []string
	for _, c := range n.Children {
		names = append(names, c.Name)
	}
	return names
}

// render serialises an object the way the Retrieve page shows it: the id as a
// root attribute, plus server-populated elements the client never sent.
// Simulated: doubles are rendered with two decimals, like 100000.00 in the
// documented insert example, to prove that formatting causes no diff.
func (f *fakeAPI) render(id string, obj *xnode) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<realestates:apartmentRent xmlns:ns2="http://rest.immobilienscout24.de/schema/platform/gis/1.0" ` +
		`xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" ` +
		`xmlns:realestates="` + realEstatesNamespace + `" id="` + id + `">`)

	types := map[string]string{}
	for _, e := range f.topOrder {
		types[e.Name] = e.Type
	}
	for _, c := range obj.Children {
		out := *c
		if types[c.Name] == "xs:double" {
			if v, err := strconv.ParseFloat(c.Text, 64); err == nil {
				out.Text = strconv.FormatFloat(v, 'f', 2, 64)
			}
		}
		switch c.Name {
		case "address":
			out.Children = append(append([]*xnode{}, c.Children...), &xnode{Name: "geoHierarchy", Children: []*xnode{
				{Name: "city", Children: []*xnode{{Name: "geoCodeId", Text: "1"}}},
			}})
		case "showAddress":
			// The documented GET example carries attachments before showAddress.
			writeNode(&b, &xnode{Name: "attachments", Attrs: []xml.Attr{{
				Name:  xml.Name{Local: "xlink:href"},
				Value: f.BaseURL() + "/offer/v1.0/user/me/realestate/" + id + "/attachment",
			}}})
		}
		writeNode(&b, &out)
		if c.Name == "title" {
			writeNode(&b, &xnode{Name: "creationDate", Text: "2026-09-29T10:00:00.000+02:00"})
			writeNode(&b, &xnode{Name: "lastModificationDate", Text: "2026-09-29T10:00:00.000+02:00"})
		}
	}
	b.WriteString(`</realestates:apartmentRent>`)
	return b.String()
}

func writeNode(b *strings.Builder, n *xnode) {
	b.WriteString("<" + n.Name)
	for _, a := range n.Attrs {
		b.WriteString(" " + a.Name.Local + `="`)
		_ = xml.EscapeText(b, []byte(a.Value))
		b.WriteString(`"`)
	}
	b.WriteString(">")
	if len(n.Children) == 0 {
		_ = xml.EscapeText(b, []byte(n.Text))
	}
	for _, c := range n.Children {
		writeNode(b, c)
	}
	b.WriteString("</" + n.Name + ">")
}

func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml;charset=UTF-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// writeMessages writes a <common:messages> body in the documented error shape.
func writeMessages(w http.ResponseWriter, status int, code, text string) {
	var b strings.Builder
	b.WriteString(`<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"><message><messageCode>` + code + `</messageCode><message>`)
	_ = xml.EscapeText(&b, []byte(text))
	b.WriteString(`</message></message></common:messages>`)
	writeRaw(w, status, b.String())
}

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

// sandboxDefaults are the values the live sandbox sets for fields a create
// request omits.
var sandboxDefaults = [][2]string{
	{"apartmentType", "NO_INFORMATION"},
	{"lift", "false"},
	{"cellar", "NOT_APPLICABLE"},
	{"heatingCostsInServiceCharge", "NOT_APPLICABLE"},
	{"petsAllowed", "NO_INFORMATION"},
	{"builtInKitchen", "false"},
	{"balcony", "false"},
	{"garden", "false"},
}
