package immobilienscout24

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Verbatim response bodies from the API documentation.
const (
	// https://api.immobilienscout24.de/api-docs/import-export/real-estate/insert-real-estate/
	documentedCreatedBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"
    xmlns:xlink="http://www.w3.org/1999/xlink">
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [REALESTATE] with id [123456] has been created.</message>
        <id>123456</id>
    </message>
</common:messages>`

	// https://api.immobilienscout24.de/api-docs/responses/
	documentedErrorBody = `<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0">
    <message>
        <messageCode>ERROR_RESOURCE_NOT_FOUND</messageCode>
        <message>Resource [searcher] with id [test@test.de] not found.</message>
    </message>
</common:messages>`
)

// stubServer answers every request with one canned response.
func stubServer(t *testing.T, status int, header http.Header, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "ck", "cs", "at", "ats", "test")
}

func minimalDocument() *apartmentRentDocument {
	return fullModel().toDocument()
}

func TestCreateParsesDocumentedCreatedBody(t *testing.T) {
	c := stubServer(t, http.StatusCreated, nil, documentedCreatedBody)
	id, err := c.CreateApartmentRent(context.Background(), minimalDocument())
	if err != nil {
		t.Fatal(err)
	}
	if id != "123456" {
		t.Fatalf("id = %q, want 123456", id)
	}
}

func TestCreateFallsBackToLocationThenMessageText(t *testing.T) {
	noID := strings.Replace(documentedCreatedBody, "<id>123456</id>", "", 1)
	c := stubServer(t, http.StatusCreated, http.Header{"Location": {"https://x/restapi/api/offer/v1.0/user/me/realestate/777"}}, noID)
	if id, err := c.CreateApartmentRent(context.Background(), minimalDocument()); err != nil || id != "777" {
		t.Fatalf("Location fallback: id = %q, err = %v", id, err)
	}
	c = stubServer(t, http.StatusCreated, nil, noID)
	if id, err := c.CreateApartmentRent(context.Background(), minimalDocument()); err != nil || id != "123456" {
		t.Fatalf("message text fallback: id = %q, err = %v", id, err)
	}
}

// The old resource dereferenced a nil error here and panicked.
func TestCreateWithoutAnyIDReturnsError(t *testing.T) {
	for name, body := range map[string]string{
		"messages without id": `<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"><message><messageCode>MESSAGE_RESOURCE_CREATED</messageCode><message>created</message></message></common:messages>`,
		"idless realEstate":   `<realEstate/>`,
		"empty body":          ``,
		"plain text":          `Created`,
	} {
		t.Run(name, func(t *testing.T) {
			c := stubServer(t, http.StatusCreated, nil, body)
			id, err := c.CreateApartmentRent(context.Background(), minimalDocument())
			if err == nil {
				t.Fatalf("expected an error, got id %q", id)
			}
			if !strings.Contains(err.Error(), "could not determine its id") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDocumentedErrorBodyIsParsed(t *testing.T) {
	c := stubServer(t, http.StatusNotFound, nil, documentedErrorBody)
	_, err := c.GetApartmentRent(context.Background(), "42")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 || len(apiErr.Messages) != 1 || apiErr.Messages[0].Code != "ERROR_RESOURCE_NOT_FOUND" ||
		apiErr.Messages[0].Text != "Resource [searcher] with id [test@test.de] not found." {
		t.Fatalf("unexpected parse: %+v", apiErr)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("documented 404 must match ErrNotFound")
	}
	if !strings.Contains(err.Error(), "ERROR_RESOURCE_NOT_FOUND: Resource [searcher]") {
		t.Fatalf("error text lacks the message: %v", err)
	}
}

// A 404 without a not-found message code (for example a wrong base_url, or
// the documented 404-for-bad-Accept) must not look like a deleted object.
func TestBare404IsNotNotFound(t *testing.T) {
	c := stubServer(t, http.StatusNotFound, nil, "Not Found")
	_, err := c.GetApartmentRent(context.Background(), "42")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("bare 404 must be an error but not ErrNotFound, got %v", err)
	}
	c = stubServer(t, http.StatusPreconditionFailed, nil, documentedErrorBody)
	if _, err := c.GetApartmentRent(context.Background(), "42"); errors.Is(err, ErrNotFound) {
		t.Fatal("a non-404 status must not match ErrNotFound")
	}
}

func TestErrorsAreBoundedAndCarryNoCredentials(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, strings.Repeat("Too Many Requests ", 1000))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "consumer-key-value", "consumer-secret-value", "token-value", "token-secret-value", "test")
	err := c.DeleteRealEstate(context.Background(), "1")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if len(msg) > maxErrorBodyBytes+200 {
		t.Fatalf("error is %d bytes, not bounded", len(msg))
	}
	if sawAuth == "" {
		t.Fatal("request carried no Authorization header")
	}
	for _, secret := range []string{"consumer-key-value", "consumer-secret-value", "token-value", "token-secret-value", "oauth_signature", sawAuth} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error contains %q: %s", secret, msg)
		}
	}
}

func TestRequestHeadersAndPaths(t *testing.T) {
	type seen struct{ method, path, accept, contentType string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.Method, r.URL.Path, r.Header.Get("Accept"), r.Header.Get("Content-Type")})
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, documentedCreatedBody)
		case http.MethodGet:
			_, _ = io.WriteString(w, `<realestates:apartmentRent xmlns:realestates="`+realEstatesNamespace+`" id="1"><title>t</title></realestates:apartmentRent>`)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/restapi/api/", "ck", "cs", "at", "ats", "test")
	ctx := context.Background()
	_, _ = c.CreateApartmentRent(ctx, minimalDocument())
	_, _ = c.GetApartmentRent(ctx, "1")
	_ = c.UpdateApartmentRent(ctx, "1", minimalDocument())
	_ = c.DeleteRealEstate(ctx, "1")
	want := []seen{
		{"POST", "/restapi/api/offer/v1.0/user/me/realestate/", "application/xml", "application/xml"},
		{"GET", "/restapi/api/offer/v1.0/user/me/realestate/1", "application/xml", ""},
		{"PUT", "/restapi/api/offer/v1.0/user/me/realestate/1", "application/xml", "application/xml"},
		{"DELETE", "/restapi/api/offer/v1.0/user/me/realestate/1", "application/xml", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requests: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGetRejectsOtherRealEstateTypes(t *testing.T) {
	c := stubServer(t, http.StatusOK, nil, `<realestates:houseBuy xmlns:realestates="`+realEstatesNamespace+`" id="1"><title>t</title></realestates:houseBuy>`)
	if _, err := c.GetApartmentRent(context.Background(), "1"); err == nil || !strings.Contains(err.Error(), "houseBuy") {
		t.Fatalf("expected a type mismatch error, got %v", err)
	}
}

// fullModel sets every attribute the resource models.
func fullModel() *apartmentRentModel {
	s, b, f := types.StringValue, types.BoolValue, types.Float64Value
	return &apartmentRentModel{
		ExternalID: s("tf-ext-1"), Title: s("anonymized"),
		Address: &addressModel{Street: s("Invalidenstrasse"), HouseNumber: s("65"), Postcode: s("10557"), City: s("Berlin"),
			Coordinates: &coordinatesModel{Latitude: f(52.53), Longitude: f(13.38)}},
		ShowAddress: b(true), DescriptionNote: s("d"), FurnishingNote: s("f"), LocationNote: s("l"), OtherNote: s("o"),
		ApartmentType: s("APARTMENT"), Floor: types.Int64Value(2), Lift: b(true), Cellar: s("YES"), FreeFrom: s("sofort"),
		NumberOfFloors: types.Int64Value(5), BaseRent: f(900.5), TotalRent: f(1200), ServiceCharge: f(200), Deposit: s("3 Kaltmieten"),
		HeatingCosts: f(99.5), HeatingCostsInServiceCharge: s("NO"), PetsAllowed: s("NEGOTIABLE"), LivingSpace: f(65.5),
		NumberOfRooms: f(2.5), BuiltInKitchen: b(true), Balcony: b(false), Garden: b(false),
		Courtage: &courtageModel{HasCourtage: s("YES"), Courtage: s("2,38 Monatsmieten"), CourtageNote: s("n")},
	}
}

// topLevelChildren returns the names of the root's child elements, and fails
// if the root is not the documented one.
func topLevelChildren(t *testing.T, body []byte) (root xml.Name, names []string) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return root, names
		}
		if err != nil {
			t.Fatal(err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			switch depth {
			case 0:
				root = tok.Name
			case 1:
				names = append(names, tok.Name.Local)
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

func TestMarshalledOrderFollowsXSD(t *testing.T) {
	body, err := marshalApartmentRent(fullModel().toDocument())
	if err != nil {
		t.Fatal(err)
	}
	root, names := topLevelChildren(t, body)
	if root.Space != realEstatesNamespace || root.Local != "apartmentRent" {
		t.Fatalf("root = %+v", root)
	}
	if !bytes.Contains(body, []byte(`<realestates:apartmentRent xmlns:realestates="`+realEstatesNamespace+`">`)) {
		t.Fatalf("root is not in the documented prefixed form:\n%s", body)
	}
	if err := checkOrder("apartmentRent", names, mustElements(t, realEstatesNamespace, "ApartmentRent")); err != nil {
		t.Fatalf("%v\nsent: %v", err, names)
	}
	// Every modelled field must actually be on the wire.
	if len(names) != 27 {
		t.Fatalf("full model produced %d elements, want 27: %v", len(names), names)
	}
	// And the fake, which the acceptance tests rely on, must agree.
	if _, err := newFakeAPI(t).validate(body); err != nil {
		t.Fatalf("fake API rejects the body: %v", err)
	}
}

func TestFakeRejectsWrongRootAndOrder(t *testing.T) {
	f := newFakeAPI(t)
	good, _ := marshalApartmentRent(fullModel().toDocument())
	for name, body := range map[string]string{
		"bare realEstate root": `<realEstate><title>t</title></realEstate>`,
		"default namespace":    strings.Replace(strings.Replace(string(good), "realestates:apartmentRent", "apartmentRent", 2), "xmlns:realestates", "xmlns", 1),
		"missing title":        strings.Replace(string(good), "<title>anonymized</title>", "", 1),
		"title after address":  strings.Replace(strings.Replace(string(good), "<title>anonymized</title>", "", 1), "</address>", "</address><title>anonymized</title>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.validate([]byte(body)); err == nil {
				t.Fatalf("fake accepted:\n%s", body)
			}
		})
	}
}

func TestFromDocumentKeepsPriorNumbersAndParsesNormalised(t *testing.T) {
	prior := fullModel()
	// Terraform hands the provider 521.22 as a 512-bit decimal, which is not
	// the same big.Float as float64(521.22). Keeping the prior value keeps the
	// exact configuration value in state.
	exact, _, err := big.ParseFloat("521.22", 10, 512, big.ToNearestEven)
	if err != nil {
		t.Fatal(err)
	}
	v, err := types.Float64Type.ValueFromTerraform(context.Background(), tftypes.NewValue(tftypes.Number, exact))
	if err != nil {
		t.Fatal(err)
	}
	exactValue, ok := v.(types.Float64)
	if !ok {
		t.Fatalf("ValueFromTerraform returned %T", v)
	}
	prior.BaseRent = exactValue
	if prior.BaseRent.Equal(types.Float64Value(521.22)) {
		t.Fatal("test precondition: the 512-bit value must differ from the float64 value")
	}

	doc := prior.toDocument()
	twoDecimals := "521.220"
	doc.BaseRent = &twoDecimals
	floor := "2.0"
	doc.Floor = &floor
	m, err := fromDocument("1", doc, prior)
	if err != nil {
		t.Fatal(err)
	}
	if !m.BaseRent.Equal(prior.BaseRent) {
		t.Fatalf("base_rent = %v, want the prior value kept", m.BaseRent)
	}
	if m.Floor.ValueInt64() != 2 {
		t.Fatalf("floor = %v", m.Floor)
	}
	changed := "600.00"
	doc.BaseRent = &changed
	if m, _ := fromDocument("1", doc, prior); m.BaseRent.ValueFloat64() != 600 {
		t.Fatalf("a changed number must be taken from the API, got %v", m.BaseRent)
	}
	bad := "abc"
	doc.LivingSpace = &bad
	if _, err := fromDocument("1", doc, prior); err == nil {
		t.Fatal("expected an error for a non-numeric livingSpace")
	}
}
