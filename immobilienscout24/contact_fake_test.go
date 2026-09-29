package immobilienscout24

// Unit tests that hold the contact side of the fake API, on which the
// acceptance tests rely, to what the live sandbox did on 2026-09-29, and a
// test of Delete against it.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func fakeClient(f *fakeAPI) *Client {
	return NewClient(f.BaseURL(), fakeConsumerKey, fakeConsumerSecret, fakeAccessToken, fakeAccessTokenSecret, "test")
}

// The fake answers with the bodies the sandbox returned, fills in what the
// sandbox filled in, returns both phone number forms and replaces the whole
// contact on PUT.
func TestFakeContactBodiesAreTheObservedOnes(t *testing.T) {
	ctx := context.Background()
	f := newFakeAPI(t)
	c := fakeClient(f)
	send := func(method, path, body string) *response {
		t.Helper()
		var b []byte
		if body != "" {
			b = []byte(body)
		}
		resp, err := c.do(ctx, method, path, b)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	get := func(path string) string {
		t.Helper()
		return string(send(http.MethodGet, path, "").body)
	}
	a, b := contactPath+"/124309506", contactPath+"/124309508"

	if got := get(contactPath); got != anonymisedContactListBody {
		t.Errorf("list =\n%s\nwant\n%s", got, anonymisedContactListBody)
	}
	if got := get(contactPath + "/" + fakeDefaultContactID); got != anonymisedDefaultContactBody {
		t.Errorf("default contact =\n%s\nwant\n%s", got, anonymisedDefaultContactBody)
	}

	created := send(http.MethodPost, contactPath, observedContactARequest)
	if created.status != http.StatusCreated || string(created.body) != observedContactCreatedBody ||
		created.location != f.BaseURL()+"/offer/v1.0/user/me/contact/124309506" {
		t.Errorf("POST answered %d, Location %q, body\n%s", created.status, created.location, created.body)
	}
	if got := get(a); got != observedContactABody {
		t.Errorf("contact A =\n%s\nwant\n%s", got, observedContactABody)
	}

	// Contact B got this id on the sandbox.
	f.mu.Lock()
	f.contacts.nextID = 124309508
	f.mu.Unlock()
	send(http.MethodPost, contactPath, observedContactBRequest)
	if got := get(b); got != observedContactBBody {
		t.Errorf("contact B =\n%s\nwant\n%s", got, observedContactBBody)
	}

	// Sent combined, a phone number comes back split as well.
	send(http.MethodPut, a, observedContactAFullRequest)
	full := get(a)
	for _, want := range []string{
		"<firstname>anonymized</firstname>", "<phoneNumberCountryCode>+49</phoneNumberCountryCode>",
		"<phoneNumberAreaCode>30</phoneNumberAreaCode>", "<phoneNumberSubscriber>24301999</phoneNumberSubscriber>",
		"<phoneNumber>+49 30 24301999</phoneNumber>",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("GET after the PUT lacks %s:\n%s", want, full)
		}
	}
	// A PUT without them removes the first name and the phone number.
	send(http.MethodPut, a, observedContactARequest)
	if got := get(a); got != observedContactABody {
		t.Errorf("contact A after a minimal PUT =\n%s\nwant\n%s", got, observedContactABody)
	}
}

// The contact of a listing (facts 5 to 7) and the default contact (fact 10),
// through the provider's client.
func TestFakeContactListingAndDefaultRules(t *testing.T) {
	ctx := context.Background()
	f := newFakeAPI(t)
	c := fakeClient(f)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	contact := func(name string) *contactModel {
		return &contactModel{Email: types.StringValue("tf-probe-" + name + "@is24-test.de"),
			Lastname: types.StringValue("anonymized"), ExternalID: types.StringValue("tf-probe-" + name)}
	}
	isDefault := func(id string) bool {
		t.Helper()
		doc, err := c.GetContact(ctx, id)
		must(err)
		return doc.DefaultContact != nil && *doc.DefaultContact
	}

	z, err := c.CreateContact(ctx, contact("z").toDocument(false))
	must(err)
	apartment := fullModel()
	apartment.ContactID = types.StringValue(z)
	listing, err := c.CreateApartmentRent(ctx, apartment.toDocument())
	must(err)
	renders := func(snippet string) {
		t.Helper()
		obj, _ := f.Object(listing)
		if body := f.render(listing, obj); !strings.Contains(body, snippet) {
			t.Fatalf("GET of the listing lacks %s:\n%s", snippet, body)
		}
	}
	renders(`<contact id="` + z + `" externalId="tf-probe-z">`)
	if body := f.RenderContact(z); !strings.Contains(body, "<realEstateReferenceCount>1</realEstateReferenceCount>") {
		t.Fatalf("the listing does not count as a reference:\n%s", body)
	}
	// A PUT without a contact resets the listing to the default contact,
	// which has no externalId.
	apartment.ContactID = types.StringNull()
	must(c.UpdateApartmentRent(ctx, listing, apartment.toDocument()))
	renders(`<contact id="` + fakeDefaultContactID + `">`)
	// Deleting a contact moves its listings to the default contact.
	apartment.ContactID = types.StringValue(z)
	must(c.UpdateApartmentRent(ctx, listing, apartment.toDocument()))
	must(c.DeleteContact(ctx, z))
	if got := f.ListingContact(listing); got != fakeDefaultContactID {
		t.Fatalf("after deleting its contact, the listing has contact %s, want the default", got)
	}

	// Written with defaultContact true, a contact takes the flag.
	x, err := c.CreateContact(ctx, contact("x").toDocument(true))
	must(err)
	if !isDefault(x) || isDefault(fakeDefaultContactID) {
		t.Fatal("a POST with defaultContact true did not move the default")
	}
	// Left out, the flag stays, and false is ignored.
	must(c.UpdateContact(ctx, x, contact("x").toDocument(false)))
	_, err = c.do(ctx, http.MethodPut, contactPath+"/"+x, []byte(observedContactFalseRequest))
	must(err)
	if !isDefault(x) {
		t.Fatal("a PUT without defaultContact, or with false, unset the default")
	}
	if err := c.DeleteContact(ctx, x); !errors.Is(err, ErrDefaultContact) {
		t.Fatalf("DELETE of the default contact = %v, want ErrDefaultContact", err)
	}
	// A PUT with true moves the default too; then x can be deleted, once.
	y, err := c.CreateContact(ctx, contact("y").toDocument(false))
	must(err)
	must(c.UpdateContact(ctx, y, contact("y").toDocument(true)))
	if !isDefault(y) || isDefault(x) {
		t.Fatal("a PUT with defaultContact true did not move the default")
	}
	must(c.DeleteContact(ctx, x))
	if _, err := c.GetContact(ctx, x); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GET of a deleted contact = %v, want ErrNotFound", err)
	}
	if err := c.DeleteContact(ctx, x); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DELETE = %v, want ErrNotFound", err)
	}
	if got := f.ListingContact(listing); got != fakeDefaultContactID {
		t.Fatalf("moving the default moved the listing to contact %s", got)
	}
}

func TestFakeRejectsMalformedContactRequests(t *testing.T) {
	f := newFakeAPI(t)
	good, err := marshalContact(fullContactModel().toDocument(false))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.parseContact(good); err != nil {
		t.Fatalf("fake rejects the full contact: %v", err)
	}
	g := string(good)
	for name, body := range map[string]string{
		"default namespace": strings.Replace(strings.ReplaceAll(g, "common:realtorContactDetail", "realtorContactDetail"),
			"xmlns:common", "xmlns", 1),
		"list root":        strings.ReplaceAll(g, "common:realtorContactDetail", "common:realtorContactDetails"),
		"qualified child":  strings.ReplaceAll(g, "lastname>", "common:lastname>"),
		"missing email":    strings.Replace(g, "<email>tf-probe-full@is24-test.de</email>", "", 1),
		"missing lastname": strings.Replace(g, "<lastname>anonymized</lastname>", "", 1),
		"email after lastname": strings.Replace(strings.Replace(g, "<email>tf-probe-full@is24-test.de</email>", "", 1),
			"</lastname>", "</lastname><email>tf-probe-full@is24-test.de</email>", 1),
		"unknown element": strings.Replace(g, "</lastname>", "</lastname><mobile>1</mobile>", 1),
		"phone with 00":   strings.Replace(g, "<phoneNumber>+49 30", "<phoneNumber>0049 30", 1),
		"+49 and area 0":  strings.Replace(g, "<phoneNumber>+49 30", "<phoneNumber>+49 030", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := f.parseContact([]byte(body)); err == nil {
				t.Fatalf("fake accepted:\n%s", body)
			}
		})
	}

	apartment, err := marshalApartmentRent(fullModel().toDocument())
	if err != nil {
		t.Fatal(err)
	}
	element := `<contact id="` + fakeDefaultContactID + `"></contact>`
	if !strings.Contains(string(apartment), element) {
		t.Fatalf("the apartment does not carry %s:\n%s", element, apartment)
	}
	for name, replacement := range map[string]string{
		"unknown contact": `<contact id="1"></contact>`,
		"no id":           `<contact></contact>`,
		// The Real Estate Insertion tutorial's form, which the XSD does not allow.
		"id as a child element": `<contact><id>` + fakeDefaultContactID + `</id></contact>`,
	} {
		t.Run(name, func(t *testing.T) {
			body := strings.Replace(string(apartment), element, replacement, 1)
			if _, err := f.validate([]byte(body)); err == nil {
				t.Fatalf("fake accepted:\n%s", body)
			}
		})
	}
}

// Terraform refreshes before it deletes, so the acceptance tests never make
// Delete meet a contact that is already gone. Delete must count that as done,
// report the refusal to delete the default contact in its own words, and fail
// on any other error.
func TestContactDeleteHandlesGoneAndDefaultContacts(t *testing.T) {
	ctx := context.Background()
	f := newFakeAPI(t)
	r := &contactResource{client: fakeClient(f)}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	deleteContact := func(id string) resource.DeleteResponse {
		t.Helper()
		state := tfsdk.State{Schema: schema.Schema}
		if diags := state.Set(ctx, &contactModel{ID: types.StringValue(id), Email: types.StringValue("tf-probe@is24-test.de"),
			Lastname: types.StringValue("anonymized")}); diags.HasError() {
			t.Fatal(diags)
		}
		var resp resource.DeleteResponse
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
		return resp
	}

	id := f.AddContactOutOfBand("tf-probe-delete@is24-test.de", "")
	for i := range 2 {
		if resp := deleteContact(id); resp.Diagnostics.HasError() {
			t.Fatalf("delete %d: %v", i+1, resp.Diagnostics)
		}
	}
	if n := len(f.Requests(http.MethodDelete)); n != 2 {
		t.Fatalf("%d DELETE requests reached the API, want 2", n)
	}
	resp := deleteContact(fakeDefaultContactID)
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "Cannot delete the default contact" ||
		!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "default_contact = true on another immobilienscout24_contact") {
		t.Fatalf("deleting the default contact: %v", resp.Diagnostics)
	}
	r.client = stubServer(t, http.StatusInternalServerError, nil, "Internal Server Error")
	if resp := deleteContact(id); !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "Error deleting contact" {
		t.Fatalf("a 500 must fail the delete, got %v", resp.Diagnostics)
	}
}
