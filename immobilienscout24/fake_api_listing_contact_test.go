package immobilienscout24

// The contact of a real estate, on the fake API in fake_api_test.go. As on
// the live sandbox (2026-09-29), a real estate written without <contact> gets
// the default contact, also on PUT, and its GET names the contact as
// <contact id="..." externalId="..."/>, the externalId only when the contact
// has one. The contacts themselves are in fake_api_contact_test.go.
//
// A real estate that names a contact that does not exist is refused with
// 412 ERROR_RESOURCE_VALIDATION "contact.id : {id} : Contact invalid", as the
// sandbox answered for a deleted contact (2026-09-29).

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// errContactInvalid is the sandbox's answer to a real estate that names a
// contact that does not exist.
type errContactInvalid struct{ id string }

func (e errContactInvalid) Error() string {
	return "Error while validating input for the resource. [MESSAGE: contact.id : " + e.id + " : Contact invalid]"
}

// writeValidationError answers a refused real estate body: the observed 412
// for an unknown contact, the simulated schema error otherwise.
func writeValidationError(w http.ResponseWriter, err error) {
	var ci errContactInvalid
	if errors.As(err, &ci) {
		writeMessages(w, http.StatusPreconditionFailed, codeResourceValidation, ci.Error())
		return
	}
	writeMessages(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", err.Error())
}

// ListingContact returns the id of the contact of a real estate.
func (f *fakeAPI) ListingContact(realEstateID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contacts.listings[realEstateID]
}

// SetListingContactOutOfBand gives a real estate another contact, as if
// chosen on the website.
func (f *fakeAPI) SetListingContactOutOfBand(realEstateID, contactID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts.listings[realEstateID] = contactID
}

// checkListingContact checks the contact element of a real estate request: an
// empty element naming an existing contact by its numeric id, the form the XSD
// and the sandbox use.
func (f *fakeAPI) checkListingContact(root *xnode) error {
	c := root.child("contact")
	if c == nil {
		return nil
	}
	if len(c.Children) > 0 || strings.TrimSpace(c.Text) != "" {
		return fmt.Errorf("contact: the XSD declares an empty element with an id attribute")
	}
	id := c.attr("id")
	if !isDigits(id) {
		return fmt.Errorf("contact: needs a numeric id attribute, got %q", id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.contacts.contacts[id]; !ok {
		return errContactInvalid{id: id}
	}
	return nil
}

// assignListingContact takes the contact element out of a stored real estate
// and records the listing's contact: the one it names, or the default contact
// when it names none. The caller holds f.mu.
func (f *fakeAPI) assignListingContact(realEstateID string, obj *xnode) {
	contactID := f.contacts.defaultID
	for i, c := range obj.Children {
		if c.Name == "contact" {
			contactID = c.attr("id")
			obj.Children = append(obj.Children[:i], obj.Children[i+1:]...)
			break
		}
	}
	f.contacts.listings[realEstateID] = contactID
}

// writeListingContact writes the contact element of a real estate GET. The
// caller holds f.mu.
func (f *fakeAPI) writeListingContact(b *strings.Builder, realEstateID string) {
	id, ok := f.contacts.listings[realEstateID]
	if !ok {
		return
	}
	n := &xnode{Name: "contact", Attrs: []xml.Attr{{Name: xml.Name{Local: "id"}, Value: id}}}
	if c := f.contacts.contacts[id]; c != nil && c.fields["externalId"] != "" {
		n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: "externalId"}, Value: c.fields["externalId"]})
	}
	writeNode(b, n)
}

func (n *xnode) attr(local string) string {
	for _, a := range n.Attrs {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
