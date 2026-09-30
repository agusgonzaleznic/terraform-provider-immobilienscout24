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
//   - a request root of one of the four listing types in the documented
//     namespace, unqualified children, and child order and required elements
//     checked against the live XSD fixture (see xsd_test.go), not a
//     hand-written list.
//
// Behaviour the documentation does not pin down is simulated and marked
// "simulated" below. The listing types, with what the sandbox fills in, are in
// fake_api_listing_test.go, and their energy fields in fake_api_energy_test.go.
// The publish resource is in fake_api_publish_test.go, the
// contact resource and the contact of a real estate in
// fake_api_contact_test.go, the attachments in fake_api_attachment_test.go,
// the OAuth check in fake_oauth_test.go, and the XML helpers in
// fake_api_xml_test.go.

import (
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
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

	// listing holds the listing types, see fake_api_listing_test.go.
	listing fakeListingSchema

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
	// inGet holds texts that every GET of a listing returns instead of those
	// stored, by top-level element; see ReturnInGet.
	inGet map[string]string
	// pub holds the publications, see fake_api_publish_test.go.
	pub fakePublishState
	// contacts holds the contacts, see fake_api_contact_test.go.
	contacts fakeContactState
	// att holds the attachments, see fake_api_attachment_test.go.
	att fakeAttachmentState
}

func newFakeAPI(t testing.TB) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		t:                t,
		nextID:           315000001,
		objects:          map[string]*xnode{},
		deletedOutOfBand: map[string]bool{},
		inGet:            map[string]string{},
		pub:              fakePublishState{publications: map[string]fakePublication{}},
		contacts:         newFakeContactState(t),
		att:              newFakeAttachmentState(t),
		listing:          newFakeListingSchema(t),
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
	delete(f.contacts.listings, id)
	f.deletedOutOfBand[id] = true
	f.pub.removedOutOfBand = append(f.pub.removedOutOfBand, f.removePublications(id)...)
	f.removeAttachments(id, true)
}

// ReturnInGet makes every GET of a listing return text for a top-level
// element instead of what is stored, as a broken API would; an empty text
// ends that.
func (f *fakeAPI) ReturnInGet(element, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if text == "" {
		delete(f.inGet, element)
		return
	}
	f.inGet[element] = text
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
	// Attachments also take multipart bodies, so they check the media type
	// themselves.
	if f.handleAttachment(w, r, body) {
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != mediaTypeXML {
			writeMessages(w, http.StatusUnsupportedMediaType, "ERROR_COMMON_MEDIA_TYPE_UNSUPPORTED", "Unsupported media type.")
			return
		}
	}

	id, isItem := strings.CutPrefix(r.URL.Path, fakeCollectionPath)
	newSources := r.URL.Query().Get(fakeNewEnergySourcesParam) == "true"
	switch {
	case r.URL.Path == fakeCollectionPath && r.Method == http.MethodPost:
		f.create(w, body, newSources)
	case isItem && id != "" && !strings.Contains(id, "/"):
		f.item(w, r.Method, id, body, newSources)
	case r.URL.Path == fakePublishPath || strings.HasPrefix(r.URL.Path, fakePublishPath+"/"):
		f.handlePublish(w, r.Method, r.URL.Path, body)
	case r.URL.Path == fakeContactPath || strings.HasPrefix(r.URL.Path, fakeContactPath+"/"):
		f.handleContact(w, r.Method, r.URL.Path, body)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

// create inserts a listing; newSources tells whether the request carries the
// query parameter for the newer energy sources.
func (f *fakeAPI) create(w http.ResponseWriter, body []byte, newSources bool) {
	obj, err := f.validate(body)
	if err == nil && !newSources {
		err = checkNewEnergySources(obj)
	}
	if err != nil {
		writeValidationError(w, err)
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

// item serves one listing; newSources as for create.
func (f *fakeAPI) item(w http.ResponseWriter, method, id string, body []byte, newSources bool) {
	f.mu.Lock()
	obj, exists := f.objects[id]
	f.mu.Unlock()
	if !exists {
		writeMessages(w, http.StatusNotFound, "ERROR_RESOURCE_NOT_FOUND", "Resource [realestate] with id ["+id+"] not found.")
		return
	}

	switch method {
	case http.MethodGet:
		writeRaw(w, http.StatusOK, f.renderListing(id, obj, newSources))
	case http.MethodPut:
		updated, err := f.validate(body)
		if err == nil && updated.Name != obj.Name {
			// Simulated: whether a PUT can change the type of a listing was not observed.
			err = fmt.Errorf("fake API: listing %s is a realestates:%s, not a realestates:%s", id, obj.Name, updated.Name)
		}
		if err == nil && !newSources {
			err = checkNewEnergySources(updated)
		}
		if err != nil {
			writeValidationError(w, err)
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
		delete(f.contacts.listings, id)
		f.removePublications(id)
		f.removeAttachments(id, false)
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
	f.assignListingContact(id, obj)
	// Insert page: without externalId "we'll set the scout object id
	// automatically as externalId".
	if obj.child("externalId") == nil {
		obj.Children = append([]*xnode{{Name: "externalId", Text: id}}, obj.Children...)
	}
	// The sandbox fills in what a request leaves out (observed 2026-09-29 and
	// 2026-09-30); GET returns it like any other field.
	f.completeListing(obj)
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
