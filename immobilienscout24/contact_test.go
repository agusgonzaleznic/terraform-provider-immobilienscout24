package immobilienscout24

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Requests of the sandbox probe and responses of the live sandbox, 2026-09-29,
// verbatim. Contact A was created with an email address and a last name only,
// contact B with the phone number split into its parts.
const (
	observedContactLocation    = "https://rest.sandbox-immobilienscout24.de/restapi/api/offer/v1.0/user/me/contact/124309506"
	observedContactCreatedBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [contact] with id [124309506] has been created.</message>
        <id>124309506</id>
    </message>
</common:messages>
`
	observedContactARequest = `<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <email>tf-probe-a@is24-test.de</email>
    <lastname>anonymized</lastname>
</common:realtorContactDetail>
`
	observedContactABody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" id="124309506">
    <email>tf-probe-a@is24-test.de</email>
    <salutation>NO_SALUTATION</salutation>
    <lastname>anonymized</lastname>
    <defaultContact>false</defaultContact>
    <localPartnerContact>false</localPartnerContact>
    <businessCardContact>false</businessCardContact>
    <realEstateReferenceCount>0</realEstateReferenceCount>
    <showOnProfilePage>false</showOnProfilePage>
</common:realtorContactDetail>
`
	observedContactBRequest = `<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <email>tf-probe-b@is24-test.de</email>
    <salutation>NO_SALUTATION</salutation>
    <firstname>anonymized</firstname>
    <lastname>anonymized</lastname>
    <phoneNumberCountryCode>+49</phoneNumberCountryCode>
    <phoneNumberAreaCode>30</phoneNumberAreaCode>
    <phoneNumberSubscriber>24301999</phoneNumberSubscriber>
    <address>
        <street>Invalidenstrasse</street>
        <houseNumber>65</houseNumber>
        <postcode>10557</postcode>
        <city>Berlin</city>
    </address>
    <countryCode>DEU</countryCode>
    <externalId>tf-probe-b</externalId>
</common:realtorContactDetail>
`
	observedContactBBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" id="124309508">
    <email>tf-probe-b@is24-test.de</email>
    <salutation>NO_SALUTATION</salutation>
    <firstname>anonymized</firstname>
    <lastname>anonymized</lastname>
    <phoneNumberCountryCode>+49</phoneNumberCountryCode>
    <phoneNumberAreaCode>30</phoneNumberAreaCode>
    <phoneNumberSubscriber>24301999</phoneNumberSubscriber>
    <phoneNumber>+49 30 24301999</phoneNumber>
    <address>
        <street>Invalidenstrasse</street>
        <houseNumber>65</houseNumber>
        <postcode>10557</postcode>
        <city>Berlin</city>
    </address>
    <countryCode>DEU</countryCode>
    <defaultContact>false</defaultContact>
    <localPartnerContact>false</localPartnerContact>
    <businessCardContact>false</businessCardContact>
    <realEstateReferenceCount>0</realEstateReferenceCount>
    <externalId>tf-probe-b</externalId>
    <showOnProfilePage>false</showOnProfilePage>
</common:realtorContactDetail>
`
	// A later PUT of contact A, with the phone number in the combined form.
	observedContactAFullRequest = `<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <email>tf-probe-a@is24-test.de</email>
    <firstname>anonymized</firstname>
    <lastname>anonymized</lastname>
    <phoneNumber>+49 30 24301999</phoneNumber>
</common:realtorContactDetail>
`
	// A PUT that tried to unset the default flag.
	observedContactFalseRequest = `<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <email>tf-probe-c@is24-test.de</email>
    <lastname>anonymized</lastname>
    <defaultContact>false</defaultContact>
</common:realtorContactDetail>
`
	// Message code and text as observed, in the envelope of the Responses page.
	observedDefaultContactRefusedBody = `<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0">
    <message>
        <messageCode>ERROR_RESOURCE_VALIDATION</messageCode>
        <message>` + fakeDefaultContactRefused + `</message>
    </message>
</common:messages>`
)

// The sandbox's GET of the account's default contact and of the contact list,
// with the email address and the phone number of that contact anonymised.
const (
	anonymisedDefaultContactBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" id="124308575">
    <email>tf-default@is24-test.de</email>
    <salutation>NO_SALUTATION</salutation>
    <firstname>first name</firstname>
    <lastname>last name</lastname>
    <phoneNumberCountryCode>+49</phoneNumberCountryCode>
    <phoneNumberAreaCode>30</phoneNumberAreaCode>
    <phoneNumberSubscriber>24301999</phoneNumberSubscriber>
    <phoneNumber>+49 30 24301999</phoneNumber>
    <countryCode>DEU</countryCode>
    <defaultContact>true</defaultContact>
    <localPartnerContact>false</localPartnerContact>
    <businessCardContact>false</businessCardContact>
    <realEstateReferenceCount>0</realEstateReferenceCount>
    <showOnProfilePage>false</showOnProfilePage>
</common:realtorContactDetail>
`
	anonymisedContactListBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:realtorContactDetailsList xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
    <realtorContactDetails id="124308575">
        <email>tf-default@is24-test.de</email>
        <salutation>NO_SALUTATION</salutation>
        <firstname>first name</firstname>
        <lastname>last name</lastname>
        <phoneNumberCountryCode>+49</phoneNumberCountryCode>
        <phoneNumberAreaCode>30</phoneNumberAreaCode>
        <phoneNumberSubscriber>24301999</phoneNumberSubscriber>
        <phoneNumber>+49 30 24301999</phoneNumber>
        <countryCode>DEU</countryCode>
        <defaultContact>true</defaultContact>
        <localPartnerContact>false</localPartnerContact>
        <businessCardContact>false</businessCardContact>
        <realEstateReferenceCount>0</realEstateReferenceCount>
        <showOnProfilePage>false</showOnProfilePage>
    </realtorContactDetails>
</common:realtorContactDetailsList>
`
)

// documentedMaximalContact is the "Maximum XML" of the Create a Contact page,
// verbatim.
const documentedMaximalContact = `<?xml version="1.0" encoding="UTF-8"?>
<common:realtorContactDetail xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:ns4="http://rest.immobilienscout24.de/schema/customer/1.0" xmlns:ns5="http://rest.immobilienscout24.de/schema/user/1.0" >
    <email>max.mustermann@immobilienscout24.de</email>
    <salutation>FEMALE</salutation>
    <firstname>Maxine</firstname>
    <lastname>Mustermann</lastname>
    <faxNumberCountryCode>+49</faxNumberCountryCode>
    <faxNumberAreaCode>30</faxNumberAreaCode>
    <faxNumberSubscriber>243010001</faxNumberSubscriber>
    <phoneNumberCountryCode>+49</phoneNumberCountryCode>
    <phoneNumberAreaCode>30</phoneNumberAreaCode>
    <phoneNumberSubscriber>243010001</phoneNumberSubscriber>
    <cellPhoneNumberCountryCode>+49</cellPhoneNumberCountryCode>
    <cellPhoneNumberAreaCode>179</cellPhoneNumberAreaCode>
    <cellPhoneNumberSubscriber>24301000</cellPhoneNumberSubscriber>
    <address>
        <street>Andreasstr.</street>
        <houseNumber>10</houseNumber>
        <postcode>10243</postcode>
        <city>Berlin</city>
    </address>
    <countryCode>DEU</countryCode>
    <title>Master</title>
    <additionName>HuiBuh</additionName>
    <company>ImmobilienScout24, field is no longer used or visible on the is24 website</company>
    <homepageUrl>http://www.immobilienscout24.de</homepageUrl>
    <position>position oder function in company</position>
    <secondaryEmail>secondary-email@example.com</secondaryEmail>
    <officeHours>Von 11:30 bis 12:00, dabei eine halbe Stunde Pause, field is no longer used or visible on the is24 website</officeHours>
    <defaultContact>false</defaultContact>
    <localPartnerContact>false</localPartnerContact>
    <businessCardContact>false</businessCardContact>
    <externalId>a-001</externalId>
    <showOnProfilePage>true</showOnProfilePage>   
</common:realtorContactDetail>
`

// fullContactModel sets every attribute the resource models.
func fullContactModel() *contactModel {
	s, b := types.StringValue, types.BoolValue
	return &contactModel{
		Email: s("tf-probe-full@is24-test.de"), Salutation: s("FEMALE"), Firstname: s("anonymized"), Lastname: s("anonymized"),
		Title: s("Dr."), AdditionName: s("anonymized"), PhoneNumber: s("+49 30 24301999"), CellPhoneNumber: s("+49 170 24301999"), FaxNumber: s("+49 30 24301998"),
		Address:     &contactAddressModel{Street: s("Invalidenstrasse"), HouseNumber: s("65"), Postcode: s("10557"), City: s("Berlin")},
		CountryCode: s("DEU"), HomepageURL: s("https://www.immobilienscout24.de"), Position: s("anonymized"),
		SecondaryEmail: s("tf-probe-second@is24-test.de"), ExternalID: s("tf-probe-full"), ShowOnProfilePage: b(true),
	}
}

func TestContactMarshalledOrderFollowsDocumentedOrder(t *testing.T) {
	body, err := marshalContact(fullContactModel().toDocument(true))
	if err != nil {
		t.Fatal(err)
	}
	root, names := topLevelChildren(t, body)
	if root.Space != commonNamespace || root.Local != "realtorContactDetail" {
		t.Fatalf("root = %+v", root)
	}
	if !bytes.Contains(body, []byte(`<common:realtorContactDetail xmlns:common="`+commonNamespace+`" xmlns:xlink="`+xlinkNamespace+`">`)) {
		t.Fatalf("root is not in the documented prefixed form:\n%s", body)
	}
	if err := checkOrder("realtorContactDetail", names, contactSequence()); err != nil {
		t.Fatalf("%v\nsent: %v", err, names)
	}
	// Every modelled field must actually be on the wire.
	if len(names) != 17 {
		t.Fatalf("full model produced %d elements, want 17: %v", len(names), names)
	}
	// And the fake, which the acceptance tests rely on, must agree.
	if _, _, err := newFakeAPI(t).parseContact(body); err != nil {
		t.Fatalf("fake API rejects the body: %v", err)
	}
}

// contactOrder is written by hand, so check it against every source: the
// documented example, the observed bodies, and the WADL-bundled XSD, which
// must be the same sequence without the four newer elements.
func TestContactOrderAgreesWithDocsAndXSD(t *testing.T) {
	sequence := contactSequence()
	for name, body := range map[string]string{
		"documented maximal example": documentedMaximalContact,
		"observed contact A":         observedContactABody,
		"observed contact B":         observedContactBBody,
		"observed default contact":   anonymisedDefaultContactBody,
	} {
		_, names := topLevelChildren(t, []byte(body))
		if err := checkOrder(name, names, sequence); err != nil {
			t.Error(err)
		}
	}
	var wadl []string
	for _, e := range mustElements(t, commonNamespace, "RealtorContactDetails") {
		wadl = append(wadl, e.Name)
	}
	if err := checkOrder("WADL-bundled XSD", wadl, sequence); err != nil {
		t.Error(err)
	}
	var added []string
	for _, name := range contactOrder {
		if !slices.Contains(wadl, name) {
			added = append(added, name)
		}
	}
	if got, want := strings.Join(added, ","), "position,secondaryEmail,clickOutUrl,showOnProfilePage"; got != want {
		t.Errorf("elements missing from the WADL-bundled XSD = %s, want %s", got, want)
	}
}

func TestUnmarshalContactParsesObservedBodies(t *testing.T) {
	s, f := types.StringValue, types.BoolValue(false)
	for name, tc := range map[string]struct {
		id, body string
		want     *contactModel
	}{
		"minimal contact, with the defaults the sandbox filled in": {"124309506", observedContactABody, &contactModel{
			ID: s("124309506"), Email: s("tf-probe-a@is24-test.de"), Salutation: s("NO_SALUTATION"), Lastname: s("anonymized"),
			DefaultContact: f, ShowOnProfilePage: f,
		}},
		"split phone parts are ignored, the combined number is read": {"124309508", observedContactBBody, &contactModel{
			ID: s("124309508"), Email: s("tf-probe-b@is24-test.de"), Salutation: s("NO_SALUTATION"), Firstname: s("anonymized"),
			Lastname: s("anonymized"), PhoneNumber: s("+49 30 24301999"),
			Address:     &contactAddressModel{Street: s("Invalidenstrasse"), HouseNumber: s("65"), Postcode: s("10557"), City: s("Berlin")},
			CountryCode: s("DEU"), DefaultContact: f, ExternalID: s("tf-probe-b"), ShowOnProfilePage: f,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := unmarshalContact(tc.id, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := contactFromDocument(tc.id, doc); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("model =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
	doc, err := unmarshalContact(fakeDefaultContactID, []byte(anonymisedDefaultContactBody))
	if err != nil || doc.DefaultContact == nil || !*doc.DefaultContact {
		t.Fatalf("the default contact must read as the default, got %+v, %v", doc, err)
	}
	if _, err := unmarshalContact("124309507", []byte(observedContactABody)); err == nil {
		t.Fatal("a body for another contact must be an error")
	}
	if _, err := unmarshalContact(fakeDefaultContactID, []byte(anonymisedContactListBody)); err == nil ||
		!strings.Contains(err.Error(), "realtorContactDetailsList") {
		t.Fatalf("expected a type mismatch error, got %v", err)
	}
}

func TestCheckPhoneNumber(t *testing.T) {
	for _, valid := range []string{
		"+49 30 24301999", "+49 170 24301999", "+49 30 2430 1999", "+49 30 2430-1999", "+1 212 2430199",
		// The rule about a leading 0 is only documented for +49.
		"+43 01 24301999",
		// The XSD pattern allows several spaces between the parts.
		"+49  30  24301999",
	} {
		if err := checkPhoneNumber(valid); err != nil {
			t.Errorf("checkPhoneNumber(%q) = %v, want nil", valid, err)
		}
	}
	for invalid, want := range map[string]string{
		"0049 30 24301999":  "starts with 00",
		"+49 030 24301999":  "must not start with 0",
		"+49 0 24301999":    "must not start with 0",
		"+4930 24301999":    "separated by spaces",
		"+49 3024301999":    "separated by spaces",
		"+49-30-24301999":   "separated by spaces",
		"+49 30 24301999 ":  "separated by spaces",
		" +49 30 24301999":  "separated by spaces",
		"49 30 24301999":    "separated by spaces",
		"+0 30 24301999":    "separated by spaces",
		"+49 30 2430199-":   "separated by spaces",
		"+49 30 24301999x":  "separated by spaces",
		"+49 30":            "separated by spaces",
		"+49 30 24301999\n": "separated by spaces",
	} {
		if err := checkPhoneNumber(invalid); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("checkPhoneNumber(%q) = %v, want an error containing %q", invalid, err, want)
		}
	}
}

func TestCreateContactParsesObservedResponse(t *testing.T) {
	ctx := context.Background()
	doc := fullContactModel().toDocument(false)
	c := stubServer(t, http.StatusCreated, http.Header{"Location": {observedContactLocation}}, observedContactCreatedBody)
	if id, err := c.CreateContact(ctx, doc); err != nil || id != "124309506" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
	noID := strings.Replace(observedContactCreatedBody, "<id>124309506</id>", "", 1)
	onlyLocation := strings.Replace(noID, "with id [124309506]", "with id []", 1)
	c = stubServer(t, http.StatusCreated, http.Header{"Location": {observedContactLocation}}, onlyLocation)
	if id, err := c.CreateContact(ctx, doc); err != nil || id != "124309506" {
		t.Fatalf("Location fallback: id = %q, err = %v", id, err)
	}
	c = stubServer(t, http.StatusCreated, nil, noID)
	if id, err := c.CreateContact(ctx, doc); err != nil || id != "124309506" {
		t.Fatalf("message text fallback: id = %q, err = %v", id, err)
	}
	c = stubServer(t, http.StatusCreated, nil, "Created")
	if id, err := c.CreateContact(ctx, doc); err == nil || !strings.Contains(err.Error(), "could not determine its id") {
		t.Fatalf("got id %q, err %v; want an error", id, err)
	}
}

func TestContactRequestPaths(t *testing.T) {
	type seen struct{ method, path, accept, contentType string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.Method, r.URL.Path, r.Header.Get("Accept"), r.Header.Get("Content-Type")})
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, observedContactCreatedBody)
		case http.MethodGet:
			_, _ = io.WriteString(w, observedContactABody)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/restapi/api/", "ck", "cs", "at", "ats", "test")
	ctx := context.Background()
	doc := fullContactModel().toDocument(false)
	_, _ = c.CreateContact(ctx, doc)
	_, _ = c.GetContact(ctx, "124309506")
	_ = c.UpdateContact(ctx, "124309506", doc)
	_ = c.DeleteContact(ctx, "124309506")
	want := []seen{
		{"POST", "/restapi/api/offer/v1.0/user/me/contact", "application/xml", "application/xml"},
		{"GET", "/restapi/api/offer/v1.0/user/me/contact/124309506", "application/xml", ""},
		{"PUT", "/restapi/api/offer/v1.0/user/me/contact/124309506", "application/xml", "application/xml"},
		{"DELETE", "/restapi/api/offer/v1.0/user/me/contact/124309506", "application/xml", ""},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("requests = %+v, want %+v", got, want)
	}
}

// Only the observed refusal to delete the default contact may match
// ErrDefaultContact: the status, the code and the text all have to fit.
func TestDeleteDefaultContactIsRecognised(t *testing.T) {
	ctx := context.Background()
	c := stubServer(t, http.StatusPreconditionFailed, nil, observedDefaultContactRefusedBody)
	err := c.DeleteContact(ctx, fakeDefaultContactID)
	if !errors.Is(err, ErrDefaultContact) || errors.Is(err, ErrNotFound) {
		t.Fatalf("the observed 412 must match ErrDefaultContact only, got %v", err)
	}
	if !strings.Contains(err.Error(), "default contact can not be deleted") {
		t.Fatalf("error text lacks the API's message: %v", err)
	}
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"another validation error": {http.StatusPreconditionFailed, strings.Replace(observedDefaultContactRefusedBody,
			"default contact can not be deleted", "lastname is too long", 1)},
		"another code": {http.StatusPreconditionFailed, strings.Replace(observedDefaultContactRefusedBody,
			codeResourceValidation, "ERROR_COMMON_BAD_REQUEST", 1)},
		"another status": {http.StatusBadRequest, observedDefaultContactRefusedBody},
		"not found":      {http.StatusNotFound, documentedErrorBody},
	} {
		c := stubServer(t, tc.status, nil, tc.body)
		if err := c.DeleteContact(ctx, "1"); errors.Is(err, ErrDefaultContact) {
			t.Errorf("%s: %v must not match ErrDefaultContact", name, err)
		}
	}
}

// Only true is ever sent, and only when the configuration says so.
func TestContactDocumentSendsDefaultOnlyWhenConfigured(t *testing.T) {
	m := fullContactModel()
	m.DefaultContact = types.BoolValue(true)
	body, err := marshalContact(m.toDocument(false))
	if err != nil || bytes.Contains(body, []byte("defaultContact")) {
		t.Fatalf("defaultContact sent although the configuration does not set it (%v):\n%s", err, body)
	}
	body, err = marshalContact(m.toDocument(true))
	if err != nil || !bytes.Contains(body, []byte("<defaultContact>true</defaultContact>")) {
		t.Fatalf("defaultContact true not sent (%v):\n%s", err, body)
	}
}

func TestKeepDefaultContactFlag(t *testing.T) {
	yes, no, null, unknown := types.BoolValue(true), types.BoolValue(false), types.BoolNull(), types.BoolUnknown()
	for name, tc := range map[string]struct{ state, config, plan, want types.Bool }{
		"another contact, updated":     {state: no, config: null, plan: unknown, want: no},
		"the default contact, updated": {state: yes, config: null, plan: unknown, want: unknown},
		"a new contact":                {state: null, config: null, plan: unknown, want: unknown},
		"configured":                   {state: no, config: yes, plan: yes, want: yes},
		"unchanged":                    {state: yes, config: null, plan: yes, want: yes},
	} {
		req := planmodifier.BoolRequest{Path: path.Root("default_contact"), StateValue: tc.state, ConfigValue: tc.config, PlanValue: tc.plan}
		resp := &planmodifier.BoolResponse{PlanValue: tc.plan}
		keepDefaultContactFlag{}.PlanModifyBool(context.Background(), req, resp)
		if !resp.PlanValue.Equal(tc.want) {
			t.Errorf("%s: planned %v, want %v", name, resp.PlanValue, tc.want)
		}
	}
}
