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
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
		listingModel: listingModel{
			ExternalID: s("tf-ext-1"), Title: s("anonymized"),
			Address: &addressModel{Street: s("Invalidenstrasse"), HouseNumber: s("65"), Postcode: s("10557"), City: s("Berlin"),
				Coordinates: &coordinatesModel{Latitude: f(52.53), Longitude: f(13.38)}},
			ShowAddress: b(true), ContactID: s(fakeDefaultContactID),
			DescriptionNote: s("d"), FurnishingNote: s("f"), LocationNote: s("l"), OtherNote: s("o"),
			Cellar: s("YES"), FreeFrom: s("sofort"), NumberOfFloors: types.Int64Value(5),
			Courtage: &courtageModel{HasCourtage: s("YES"), Courtage: s("2,38 Monatsmieten"), CourtageNote: s("n")},
			EnergyCertificate: &energyCertificateModel{Availability: s("AVAILABLE"), CreationDate: s("FROM_01_MAY_2014"),
				EfficiencyClass: s("B")},
			ConstructionYear: types.Int64Value(1990), HeatingType: s("CENTRAL_HEATING"),
			EnergySources:            types.SetValueMust(types.StringType, []attr.Value{s("GAS")}),
			BuildingEnergyRatingType: s("ENERGY_CONSUMPTION"), ThermalCharacteristic: f(95.5),
			EnergyConsumptionContainsWarmWater: s("NOT_APPLICABLE"),
		},
		ApartmentType: s("APARTMENT"), Floor: types.Int64Value(2), Lift: b(true),
		BaseRent: f(900.5), TotalRent: f(1200), ServiceCharge: f(200), Deposit: s("3 Kaltmieten"),
		HeatingCosts: f(99.5), HeatingCostsInServiceCharge: s("NO"), PetsAllowed: s("NEGOTIABLE"), LivingSpace: f(65.5),
		NumberOfRooms: f(2.5), BuiltInKitchen: b(true), Balcony: b(false), Garden: b(false),
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
	if len(names) != 35 {
		t.Fatalf("full model produced %d elements, want 35: %v", len(names), names)
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

// The texts of an error response cannot drive the terminal that Terraform
// prints them on: every control character but newline and tab is escaped, in
// a plain body and in the code and text of every message, and each text is
// capped like the body. The body clears the screen and prints a green "Apply
// complete!", which would make a failed apply look like a success.
func TestAPIErrorTextsCannotDriveTheTerminal(t *testing.T) {
	check := func(name string, err error, bad, want []string) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		for _, s := range bad {
			if strings.Contains(err.Error(), s) {
				t.Errorf("%s: the error contains %q:\n%s", name, s, err)
			}
		}
		for _, s := range want {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("%s: the error lacks %q:\n%s", name, s, err)
			}
		}
	}
	body := "\x1b[2J\x1b[H\x1b[32mApply complete!\x1b[0m\r\n\tnext\x7f \u009b31m \xff \x00end"
	c := stubServer(t, http.StatusBadGateway, nil, body)
	check("plain body", c.DeleteRealEstate(context.Background(), "1"),
		[]string{"\x1b", "\r", "\x7f", "\u009b", "\xff", "\x00"},
		[]string{`\x1b[2J\x1b[H\x1b[32mApply complete!\x1b[0m\x0d` + "\n\tnext", `\x7f \u009b31m \xff \x00end`})

	// XML refuses the other C0 controls, but a message can carry CR, DEL and C1.
	messages := `<common:messages xmlns:common="` + commonNamespace + `"><message><messageCode>ERROR_RESOURCE_VALIDATION` +
		"\u009b" + `2J</messageCode><message>refused` + "\u009b" + `31m&#13;` + "\x7f" + ` text</message></message></common:messages>`
	c = stubServer(t, http.StatusPreconditionFailed, nil, messages)
	check("messages", c.DeleteRealEstate(context.Background(), "1"),
		[]string{"\u009b", "\r", "\x7f"},
		[]string{`ERROR_RESOURCE_VALIDATION\u009b2J: refused\u009b31m\x0d\x7f text`})

	long := strings.Repeat("\u00e9", 1000)
	c = stubServer(t, http.StatusPreconditionFailed, nil, `<common:messages xmlns:common="`+commonNamespace+
		`"><message><messageCode>ERROR_RESOURCE_VALIDATION</messageCode><message>`+long+`</message></message></common:messages>`)
	err := c.DeleteRealEstate(context.Background(), "1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || len(apiErr.Messages) != 1 {
		t.Fatalf("expected an *APIError with one message, got %v", err)
	}
	if text := apiErr.Messages[0].Text; len(text) > maxErrorBodyBytes+len("... (truncated)") ||
		!strings.HasSuffix(text, "\u00e9... (truncated)") {
		t.Errorf("a message text of %d bytes became %d bytes: ...%s", len(long), len(text), text[len(text)-20:])
	}
}

// The ids and the attachment type that a response carries go into errors as
// well, and XML lets a CR (&#13;) and a C1 character such as CSI (&#155;)
// into them: they come out escaped there too.
func TestReturnedIDsAndTypesInErrorsAreEscaped(t *testing.T) {
	const cr, csi = "&#13;", "&#155;"
	attachment := func(typ, id string) []byte {
		return []byte(`<common:attachment xmlns:common="` + commonNamespace + `" xmlns:xsi="` + xsiNamespace +
			`" xsi:type="common:` + typ + `" id="` + id + `"></common:attachment>`)
	}
	_, attachmentErr := unmarshalAttachment("1", attachment(attachmentPicture, "2"+cr+csi))
	_, contactErr := unmarshalContact("1", []byte(`<common:realtorContactDetail xmlns:common="`+commonNamespace+
		`" id="2`+cr+csi+`"/>`))
	doc, err := unmarshalAttachment("1", attachment(attachmentPicture+cr+csi, "1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{attachmentErr, `asked for attachment 1, the API returned attachment 2\x0d\u009b`},
		{contactErr, `asked for contact 1, the API returned contact 2\x0d\u009b`},
		{wrongAttachmentType(doc.Type, "10", "1", attachmentLink), `attachment 1 of real estate 10 is a common:Picture\x0d\u009b, not a common:Link`},
	} {
		if c.err == nil || !strings.Contains(c.err.Error(), c.want) || strings.ContainsAny(c.err.Error(), "\r\u009b") {
			t.Errorf("want an error with %s and no raw CR or CSI, got %q", c.want, c.err)
		}
	}
}

// The client never follows a redirect: the request it would send on to the
// host a response names would carry a fresh OAuth signature, and an upload its
// file. A 302 or 307 fails the call with an APIError, and the target of the
// redirect gets no request, for a plain call as for an upload.
func TestClientRefusesRedirects(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer target.Close()
	ctx := context.Background()
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+r.URL.Path, status)
		}))
		c := NewClient(origin.URL, "ck", "cs", "at", "ats", "test")
		for name, call := range map[string]func() error{
			"GET": func() error {
				return c.GetRealEstate(ctx, &apartmentRentKind.realEstateType, "1", &apartmentRentDocument{})
			},
			"upload": func() error {
				_, err := c.UploadAttachment(ctx, "1", fullPictureModel().toDocument(pictureKind, anonymizedJPEGSHA256, false),
					attachmentFile{Name: "anonymized.jpg", ContentType: "image/jpeg", Content: []byte("jpeg")})
				return err
			},
		} {
			hits.Store(0)
			err := call()
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Errorf("%s answered with %d: %v, want an *APIError with that status", name, status, err)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("%s answered with %d: the redirect target got %d requests", name, status, n)
			}
		}
		origin.Close()
	}
}
