package immobilienscout24

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Responses of the live sandbox, 2026-09-29, verbatim.
const (
	observedPublishLocation    = "https://rest.sandbox-immobilienscout24.de/restapi/api/offer/v1.0/publish/325477736_10000"
	observedPublishCreatedBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [publish] with id [325477736_10000] has been created.</message>
        <id>325477736_10000</id>
    </message>
</common:messages>`
	observedPublishObjectBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:publishObject xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" id="325477736_10000">
    <realEstate id="325477736" title="anonymized" firstActivationDate="2026-09-29T22:16:58.000+02:00"/>
    <publishChannel id="10000" title="ImmobilienScout24"/>
</common:publishObject>`
	// Message code and text as observed, in the envelope of the Responses page.
	observedPublishConflictBody = `<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0">
    <message>
        <messageCode>ERROR_COMMON_REQUEST_CONFLICT</messageCode>
        <message>The operation for your request causes a conflict. [MESSAGE: Publish object already exists for publishchannel id=10001, realestate id=325477736]</message>
    </message>
</common:messages>`
	observedPublishNotFoundBody = `<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0">
    <message>
        <messageCode>ERROR_COMMON_RESOURCE_NOT_FOUND</messageCode>
        <message>Resource was not found.</message>
    </message>
</common:messages>`
)

func TestPublishSendsDocumentedBodyAndParsesObservedResponse(t *testing.T) {
	var method, path, contentType, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, contentType, body = r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(b)
		w.Header().Set("Location", observedPublishLocation)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, observedPublishCreatedBody)
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/restapi/api", "ck", "cs", "at", "ats", "test")

	id, err := c.Publish(context.Background(), "325477736", "10000")
	if err != nil {
		t.Fatal(err)
	}
	if id != "325477736_10000" {
		t.Fatalf("id = %q, want 325477736_10000", id)
	}
	if method != http.MethodPost || path != "/restapi/api/offer/v1.0/publish" || contentType != mediaTypeXML {
		t.Fatalf("request = %s %s (Content-Type %q)", method, path, contentType)
	}
	// The documented request, except that encoding/xml closes empty elements
	// with an end tag.
	want := xml.Header + `<common:publishObject xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" ` +
		`xmlns:xlink="http://www.w3.org/1999/xlink"><realEstate id="325477736"></realEstate>` +
		`<publishChannel id="10000"></publishChannel></common:publishObject>`
	if body != want {
		t.Fatalf("body =\n%s\nwant\n%s", body, want)
	}
	// And the fake, which the acceptance tests rely on, must agree.
	if re, ch, err := parsePublishRequest([]byte(body)); err != nil || re != "325477736" || ch != "10000" {
		t.Fatalf("fake API reads (%q, %q, %v)", re, ch, err)
	}
}

func TestPublishFallsBackToLocationThenMessageText(t *testing.T) {
	ctx := context.Background()
	noID := strings.Replace(observedPublishCreatedBody, "<id>325477736_10000</id>", "", 1)
	onlyLocation := strings.Replace(noID, "with id [325477736_10000]", "with id []", 1)
	c := stubServer(t, http.StatusCreated, http.Header{"Location": {observedPublishLocation}}, onlyLocation)
	if id, err := c.Publish(ctx, "325477736", "10000"); err != nil || id != "325477736_10000" {
		t.Fatalf("Location fallback: id = %q, err = %v", id, err)
	}
	c = stubServer(t, http.StatusCreated, nil, noID)
	if id, err := c.Publish(ctx, "325477736", "10000"); err != nil || id != "325477736_10000" {
		t.Fatalf("message text fallback: id = %q, err = %v", id, err)
	}
}

func TestPublishRejectsMissingOrUnexpectedID(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"no id anywhere": {`<common:messages xmlns:common="` + commonNamespace + `"><message><messageCode>MESSAGE_RESOURCE_CREATED</messageCode>` +
			`<message>created</message></message></common:messages>`, "could not determine its id"},
		"plain text":            {`Created`, "could not determine its id"},
		"id of another channel": {strings.ReplaceAll(observedPublishCreatedBody, "325477736_10000", "325477736_10001"), "reported the id 325477736_10001 instead of 325477736_10000"},
	} {
		t.Run(name, func(t *testing.T) {
			c := stubServer(t, http.StatusCreated, nil, tc.body)
			id, err := c.Publish(context.Background(), "325477736", "10000")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got id %q, err %v; want an error containing %q", id, err, tc.want)
			}
		})
	}
}

func TestPublishErrorsMatchConflictAndNotFound(t *testing.T) {
	ctx := context.Background()
	c := stubServer(t, http.StatusConflict, nil, observedPublishConflictBody)
	_, err := c.Publish(ctx, "325477736", "10001")
	if !errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
		t.Fatalf("the observed 409 must match ErrConflict only, got %v", err)
	}
	if !strings.Contains(err.Error(), "Publish object already exists for publishchannel id=10001, realestate id=325477736") {
		t.Fatalf("error text lacks the API's message: %v", err)
	}

	c = stubServer(t, http.StatusNotFound, nil, observedPublishNotFoundBody)
	if _, err := c.Publish(ctx, "1", "10000"); !errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		t.Fatalf("the observed 404 must match ErrNotFound only, got %v", err)
	}

	// The status and the code both have to fit.
	c = stubServer(t, http.StatusConflict, nil, documentedErrorBody)
	if _, err := c.Publish(ctx, "1", "10000"); errors.Is(err, ErrConflict) {
		t.Fatal("a 409 without ERROR_COMMON_REQUEST_CONFLICT must not match ErrConflict")
	}
	c = stubServer(t, http.StatusPreconditionFailed, nil, observedPublishConflictBody)
	if _, err := c.Publish(ctx, "1", "10000"); errors.Is(err, ErrConflict) {
		t.Fatal("ERROR_COMMON_REQUEST_CONFLICT without a 409 must not match ErrConflict")
	}
}

func TestGetPublicationParsesObservedBody(t *testing.T) {
	ctx := context.Background()
	c := stubServer(t, http.StatusOK, nil, observedPublishObjectBody)
	p, err := c.GetPublication(ctx, "325477736_10000")
	if err != nil {
		t.Fatal(err)
	}
	if *p != (Publication{ID: "325477736_10000", RealEstateID: "325477736", ChannelID: "10000"}) {
		t.Fatalf("publication = %+v", *p)
	}
	if _, err := c.GetPublication(ctx, "325477736_10001"); err == nil {
		t.Fatal("a body for another publication must be an error")
	}
	c = stubServer(t, http.StatusOK, nil, observedPublishConflictBody)
	if _, err := c.GetPublication(ctx, "325477736_10000"); err == nil || !strings.Contains(err.Error(), "publishObject") {
		t.Fatalf("expected a type mismatch error, got %v", err)
	}
}

func TestPublicationRequestPaths(t *testing.T) {
	type seen struct{ method, path, accept, contentType string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.Method, r.URL.Path, r.Header.Get("Accept"), r.Header.Get("Content-Type")})
		_, _ = io.WriteString(w, observedPublishObjectBody)
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/restapi/api/", "ck", "cs", "at", "ats", "test")
	_, _ = c.GetPublication(context.Background(), "325477736_10000")
	_ = c.Unpublish(context.Background(), "325477736_10000")
	want := []seen{
		{"GET", "/restapi/api/offer/v1.0/publish/325477736_10000", "application/xml", ""},
		{"DELETE", "/restapi/api/offer/v1.0/publish/325477736_10000", "application/xml", ""},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("requests = %+v, want %+v", got, want)
	}
}

// The second DELETE of a publication answered 404 ERROR_RESOURCE_NOT_FOUND on
// the sandbox. Delete relies on that matching ErrNotFound.
func TestUnpublishTwiceIsNotFound(t *testing.T) {
	c := stubServer(t, http.StatusNotFound, nil, `<common:messages xmlns:common="`+commonNamespace+`"><message>`+
		`<messageCode>ERROR_RESOURCE_NOT_FOUND</messageCode><message>Resource [publish] with id [1_10000] not found.</message></message></common:messages>`)
	if err := c.Unpublish(context.Background(), "1_10000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Terraform refreshes before it deletes, so the acceptance tests never make
// Delete meet a publication that is already gone. On the sandbox, a second
// DELETE answered 404 ERROR_RESOURCE_NOT_FOUND; Delete must count that as
// done, and still fail on any other error.
func TestPublicationDeleteTreatsNotFoundAsDone(t *testing.T) {
	ctx := context.Background()
	f := newFakeAPI(t)
	f.PublishOutOfBand("1", "10000")
	r := &publicationResource{client: NewClient(f.BaseURL(), fakeConsumerKey, fakeConsumerSecret, fakeAccessToken, fakeAccessTokenSecret, "test")}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	if diags := state.Set(ctx, &publicationModel{ID: types.StringValue("1_10000"),
		RealEstateID: types.StringValue("1"), ChannelID: types.StringValue("10000")}); diags.HasError() {
		t.Fatal(diags)
	}

	for i := range 2 {
		var resp resource.DeleteResponse
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("delete %d: %v", i+1, resp.Diagnostics)
		}
	}
	if n := len(f.Requests(http.MethodDelete)); n != 2 {
		t.Fatalf("%d DELETE requests reached the API, want 2", n)
	}

	r.client = stubServer(t, http.StatusInternalServerError, nil, "Internal Server Error")
	var resp resource.DeleteResponse
	r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("a 500 must fail the delete")
	}
}

// Terraform creates resources in parallel; Publish must still send one
// request at a time.
func TestPublishSendsOneRequestAtATime(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		realEstateID, channelID, _ := parsePublishRequest(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, fakePublishCreatedBody(publicationID(realEstateID, channelID)))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "ck", "cs", "at", "ats", "test")

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.Publish(context.Background(), strconv.Itoa(i+1), "10000")
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if maxInFlight != 1 {
		t.Fatalf("%d publish requests were in flight at once, want 1", maxInFlight)
	}
}

func TestParsePublicationID(t *testing.T) {
	for id, want := range map[string][2]string{
		"325477736_10000": {"325477736", "10000"},
		"1_10001":         {"1", "10001"},
	} {
		realEstateID, channelID, ok := parsePublicationID(id)
		if !ok || realEstateID != want[0] || channelID != want[1] {
			t.Errorf("parsePublicationID(%q) = (%q, %q, %v), want (%q, %q, true)", id, realEstateID, channelID, ok, want[0], want[1])
		}
	}
	for _, id := range []string{
		"", "325477736", "325477736_", "_10000", "_", "325477736-10000", "325477736_10000_1",
		"0325477736_10000", "325477736_010000", "0_10000", "abc_10000", " 325477736_10000", "325477736_10000\n",
	} {
		if realEstateID, channelID, ok := parsePublicationID(id); ok {
			t.Errorf("parsePublicationID(%q) = (%q, %q, true), want invalid", id, realEstateID, channelID)
		}
	}
}

// The fake API answers with the bodies the sandbox returned.
func TestFakePublishBodiesAreTheObservedOnes(t *testing.T) {
	if got := fakePublishCreatedBody("325477736_10000"); got != observedPublishCreatedBody {
		t.Errorf("201 body =\n%s\nwant\n%s", got, observedPublishCreatedBody)
	}
	if got := fakePublishObjectBody("325477736", "anonymized", "10000"); got != observedPublishObjectBody {
		t.Errorf("GET body =\n%s\nwant\n%s", got, observedPublishObjectBody)
	}
}

func TestFakeRejectsMalformedPublishRequests(t *testing.T) {
	good, err := marshalPublishRequest("325477736", "10000")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"default namespace": strings.Replace(strings.ReplaceAll(string(good), "common:publishObject", "publishObject"), "xmlns:common", "xmlns", 1),
		"wrong root":        strings.ReplaceAll(string(good), "common:publishObject", "common:publishObjects"),
		"swapped children": strings.Replace(string(good), `<realEstate id="325477736"></realEstate><publishChannel id="10000"></publishChannel>`,
			`<publishChannel id="10000"></publishChannel><realEstate id="325477736"></realEstate>`, 1),
		"qualified child": strings.ReplaceAll(string(good), "realEstate", "common:realEstate"),
		"missing id":      strings.Replace(string(good), ` id="10000"`, "", 1),
		"nested element":  strings.Replace(string(good), `></realEstate>`, `><title>t</title></realEstate>`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parsePublishRequest([]byte(body)); err == nil {
				t.Fatalf("fake accepted:\n%s", body)
			}
		})
	}
}
