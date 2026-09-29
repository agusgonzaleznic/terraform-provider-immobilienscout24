package immobilienscout24

// The contact resource of the fake API in fake_api_test.go. It mirrors what
// the live sandbox did on 2026-09-29:
//
//   - POST /offer/v1.0/user/me/contact answers 201 with a Location header and
//     a messages body that carries the new id;
//   - GET /contact/{id} fills in what the request left out: salutation
//     NO_SALUTATION, the defaultContact, localPartnerContact,
//     businessCardContact and showOnProfilePage flags false, and
//     realEstateReferenceCount. A phone number comes back both combined and
//     split, whichever form was sent;
//   - GET /contact lists the contacts in a common:realtorContactDetailsList;
//   - PUT replaces the whole contact, except that a missing defaultContact
//     keeps the flag and false is ignored;
//   - the account has exactly one default contact. The fake starts with one,
//     like the sandbox account, and a contact written with defaultContact true
//     takes the flag;
//   - DELETE answers 200, and 404 ERROR_RESOURCE_NOT_FOUND afterwards. It
//     answers 412 ERROR_RESOURCE_VALIDATION for the default contact, and moves
//     the listings of a deleted contact to the default contact;
//   - a contact's realEstateReferenceCount counts the listings that use it.
//
// The contact of a real estate is in fake_api_listing_contact_test.go.
//
// Simulated, not observed: the texts of the 404 and of the PUT and DELETE
// responses (the documented bodies are used), how invalid requests fail, and
// that the default contact's reference count includes the listings that fell
// back to it.

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	fakeContactPath = "/restapi/api/offer/v1.0/user/me/contact"
	// fakeDefaultContactID is the id of the default contact that the sandbox
	// account came with.
	fakeDefaultContactID = "124308575"
	// fakeDefaultContactRefused is the sandbox's text for a DELETE of the
	// default contact.
	fakeDefaultContactRefused = "Error while validating input for the resource. [MESSAGE: default contact can not be " +
		"deleted. Please provide assigntocontactid query parameter]"
)

// contactOrder is the sequence of common:RealtorContactDetails in IS24's
// per-file common-1.0.xsd (contacts-attachments.md, section 2.7), which the
// documented maximal example follows. The WADL-bundled XSD in testdata cannot
// be used: it lacks position, secondaryEmail, clickOutUrl and
// showOnProfilePage. TestContactOrderAgreesWithDocsAndXSD checks this list
// against both.
var contactOrder = []string{
	"email", "salutation", "firstname", "lastname",
	"faxNumberCountryCode", "faxNumberAreaCode", "faxNumberSubscriber", "faxNumber",
	"phoneNumberCountryCode", "phoneNumberAreaCode", "phoneNumberSubscriber", "phoneNumber",
	"cellPhoneNumberCountryCode", "cellPhoneNumberAreaCode", "cellPhoneNumberSubscriber", "cellPhoneNumber",
	"address", "countryCode", "title", "additionName", "company", "homepageUrl", "portraitUrl",
	"position", "secondaryEmail", "clickOutUrl", "officeHours",
	"defaultContact", "localPartnerContact", "businessCardContact", "realEstateReferenceCount",
	"externalId", "showOnProfilePage",
}

// contactSequence is contactOrder for checkOrder. Every element is optional
// in the XSD; the mandatory ones are checked separately.
func contactSequence() []xsdElement {
	sequence := make([]xsdElement, len(contactOrder))
	for i, name := range contactOrder {
		sequence[i] = xsdElement{Name: name, Optional: true}
	}
	return sequence
}

// fakePhonePattern is the XSD pattern of the combined phone numbers, kept
// apart from the provider's copy so that the fake checks independently.
var fakePhonePattern = regexp.MustCompile(`^(\+[1-9]\d{0,3}) +(\d{1,10}) +([\d][\d \-]{0,24}[\d])$`)

// fakeContact is a stored contact: its elements by name, and its address. The
// default flag and the reference count are derived, not stored.
type fakeContact struct {
	fields  map[string]string
	address *xnode
}

// fakeContactState is the contact side of fakeAPI, guarded by fakeAPI.mu.
type fakeContactState struct {
	addressOrder []xsdElement
	nextID       int
	contacts     map[string]*fakeContact
	defaultID    string
	// listings maps the id of every real estate to the id of its contact.
	listings map[string]string
	// created and deletedOutOfBand record contact history for CheckDestroy.
	created          []string
	deletedOutOfBand map[string]bool
	// stealDefault, when set, takes the default flag right after the next
	// contact write, as a concurrent write by another client would.
	stealDefault string
}

func newFakeContactState(t testing.TB) fakeContactState {
	return fakeContactState{
		addressOrder: mustElements(t, commonNamespace, "Address"),
		// The first contact the probe created had this id.
		nextID:           124309506,
		contacts:         map[string]*fakeContact{fakeDefaultContactID: fakeSandboxDefaultContact()},
		defaultID:        fakeDefaultContactID,
		listings:         map[string]string{},
		deletedOutOfBand: map[string]bool{},
	}
}

// fakeSandboxDefaultContact is the default contact the sandbox account came
// with, with its email address and phone number anonymised.
func fakeSandboxDefaultContact() *fakeContact {
	return &fakeContact{fields: map[string]string{
		"email": "tf-default@is24-test.de", "salutation": "NO_SALUTATION", "firstname": "first name", "lastname": "last name",
		"phoneNumberCountryCode": "+49", "phoneNumberAreaCode": "30", "phoneNumberSubscriber": "24301999",
		"phoneNumber": "+49 30 24301999", "countryCode": "DEU",
		"localPartnerContact": "false", "businessCardContact": "false", "showOnProfilePage": "false",
	}}
}

// ContactIDs returns the ids of every contact, sorted.
func (f *fakeAPI) ContactIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id := range f.contacts.contacts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// DefaultContactID returns the id of the default contact.
func (f *fakeAPI) DefaultContactID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contacts.defaultID
}

// ContactField returns an element of a stored contact, and whether it is set.
func (f *fakeAPI) ContactField(id, element string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.contacts.contacts[id]
	if !ok {
		return "", false
	}
	v, ok := c.fields[element]
	return v, ok
}

// ContactHistory returns the ids of the contacts created through the API, and
// the set of those that were deleted out of band.
func (f *fakeAPI) ContactHistory() ([]string, map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	oob := map[string]bool{}
	for id := range f.contacts.deletedOutOfBand {
		oob[id] = true
	}
	return append([]string(nil), f.contacts.created...), oob
}

// AddContactOutOfBand creates a contact as if on the website, and returns its id.
func (f *fakeAPI) AddContactOutOfBand(email, externalID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := strconv.Itoa(f.contacts.nextID)
	f.contacts.nextID++
	c := &fakeContact{fields: map[string]string{"email": email, "lastname": "anonymized"}}
	if externalID != "" {
		c.fields["externalId"] = externalID
	}
	c.applyDefaults()
	f.contacts.contacts[id] = c
	return id
}

// DeleteContactOutOfBand deletes a contact as if on the website.
func (f *fakeAPI) DeleteContactOutOfBand(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == f.contacts.defaultID {
		f.t.Fatalf("fake API: contact %s is the default contact, which the API refuses to delete", id)
	}
	f.removeContact(id)
	f.contacts.deletedOutOfBand[id] = true
}

// SetContactOutOfBand changes an element of a contact as if edited on the website.
func (f *fakeAPI) SetContactOutOfBand(id, element, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts.contacts[id].fields[element] = value
}

// MakeDefaultOutOfBand makes a contact the default, as if chosen on the website.
func (f *fakeAPI) MakeDefaultOutOfBand(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts.defaultID = id
}

// StealDefaultAfterNextContactWrite makes id the default contact right after
// the next contact POST or PUT, as a concurrent write would.
func (f *fakeAPI) StealDefaultAfterNextContactWrite(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts.stealDefault = id
}

// RenderContact returns the GET body of a contact.
func (f *fakeAPI) RenderContact(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renderContact(id)
}

func (f *fakeAPI) handleContact(w http.ResponseWriter, method, urlPath string, body []byte) {
	id, isItem := strings.CutPrefix(urlPath, fakeContactPath+"/")
	isItem = isItem && id != "" && !strings.Contains(id, "/")
	switch {
	case urlPath == fakeContactPath && method == http.MethodPost:
		f.createContact(w, body)
	case urlPath == fakeContactPath && method == http.MethodGet:
		f.mu.Lock()
		list := f.renderContactList()
		f.mu.Unlock()
		writeRaw(w, http.StatusOK, list)
	case isItem:
		f.contactItem(w, method, id, body)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

func (f *fakeAPI) createContact(w http.ResponseWriter, body []byte) {
	c, makeDefault, err := f.parseContact(body)
	if err != nil {
		writeMessages(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", err.Error())
		return
	}
	f.mu.Lock()
	id := strconv.Itoa(f.contacts.nextID)
	f.contacts.nextID++
	f.contacts.created = append(f.contacts.created, id)
	f.contacts.contacts[id] = c
	f.wroteContact(id, makeDefault)
	f.mu.Unlock()

	w.Header().Set("Location", f.BaseURL()+"/offer/v1.0/user/me/contact/"+id)
	writeRaw(w, http.StatusCreated, fakeContactCreatedBody(id))
}

func (f *fakeAPI) contactItem(w http.ResponseWriter, method, id string, body []byte) {
	var c *fakeContact
	var makeDefault bool
	if method == http.MethodPut {
		var err error
		if c, makeDefault, err = f.parseContact(body); err != nil {
			writeMessages(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", err.Error())
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.contacts.contacts[id]; !exists {
		// The code is the sandbox's; the text is simulated.
		writeMessages(w, http.StatusNotFound, "ERROR_RESOURCE_NOT_FOUND", "Resource [contact] with id ["+id+"] not found.")
		return
	}

	switch method {
	case http.MethodGet:
		writeRaw(w, http.StatusOK, f.renderContact(id))
	case http.MethodPut:
		f.contacts.contacts[id] = c
		f.wroteContact(id, makeDefault)
		// Verbatim from the Update a Contact page.
		writeRaw(w, http.StatusOK, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"
    xmlns:xlink="http://www.w3.org/1999/xlink">
    <message>
        <messageCode>MESSAGE_RESOURCE_UPDATED</messageCode>
        <message>Resource [NAME] with id [ID] has been updated. </message>
    </message>
</common:messages>`)
	case http.MethodDelete:
		if id == f.contacts.defaultID {
			writeMessages(w, http.StatusPreconditionFailed, codeResourceValidation, fakeDefaultContactRefused)
			return
		}
		f.removeContact(id)
		// Verbatim from the Delete a Contact page, with the fake's id.
		writeRaw(w, http.StatusOK, `<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:ns3="http://rest.immobilienscout24.de/schema/platform/gis/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
   <message>
      <messageCode>MESSAGE_RESOURCE_DELETED</messageCode>
      <message>Resource [contact] with id [`+id+`] has been deleted.</message>
      <id>`+id+`</id>
   </message>
</common:messages>`)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

// wroteContact applies the default flag of a contact write: true takes the
// flag, false and a missing element change nothing. The caller holds f.mu.
func (f *fakeAPI) wroteContact(id string, makeDefault bool) {
	if makeDefault {
		f.contacts.defaultID = id
	}
	if f.contacts.stealDefault != "" {
		f.contacts.defaultID, f.contacts.stealDefault = f.contacts.stealDefault, ""
	}
}

// removeContact deletes a contact and moves its listings to the default
// contact. The caller holds f.mu.
func (f *fakeAPI) removeContact(id string) {
	delete(f.contacts.contacts, id)
	for realEstateID, contactID := range f.contacts.listings {
		if contactID == id {
			f.contacts.listings[realEstateID] = f.contacts.defaultID
		}
	}
}

// parseContact checks a contact request against the documented order and
// mandatory elements, and returns it with the sandbox's defaults applied and
// whether it asks to become the default contact.
func (f *fakeAPI) parseContact(body []byte) (*fakeContact, bool, error) {
	root, err := parseRequest(body, commonNamespace, "realtorContactDetail")
	if err != nil {
		return nil, false, err
	}
	if err := checkOrder("realtorContactDetail", childNames(root), contactSequence()); err != nil {
		return nil, false, err
	}
	c := &fakeContact{fields: map[string]string{}}
	makeDefault := false
	for _, n := range root.Children {
		switch n.Name {
		case "address":
			if err := checkOrder("address", childNames(n), f.contacts.addressOrder); err != nil {
				return nil, false, err
			}
			for _, part := range n.Children {
				part.Text = strings.TrimSpace(part.Text)
			}
			c.address = n
		case "defaultContact":
			makeDefault = strings.TrimSpace(n.Text) == "true"
		case "realEstateReferenceCount":
			// Server-owned.
		default:
			c.fields[n.Name] = strings.TrimSpace(n.Text)
		}
	}
	// Mandatory on the Create and Update pages; the text is from Top API Errors.
	for _, required := range []string{"email", "lastname"} {
		if c.fields[required] == "" {
			return nil, false, fmt.Errorf("Element(s) [%s] must be set", required)
		}
	}
	for _, number := range []string{"faxNumber", "phoneNumber", "cellPhoneNumber"} {
		if err := c.completePhone(number); err != nil {
			return nil, false, err
		}
	}
	c.applyDefaults()
	return c, makeDefault, nil
}

// completePhone adds the split form of a phone number that was sent combined,
// and the combined form of one that was sent split: the sandbox returns both.
// A combined number must follow the documented rules.
func (c *fakeContact) completePhone(number string) error {
	combined, hasCombined := c.fields[number]
	countryCode, hasSplit := c.fields[number+"CountryCode"]
	switch {
	case hasCombined:
		m := fakePhonePattern.FindStringSubmatch(combined)
		if m == nil || (m[1] == "+49" && strings.HasPrefix(m[2], "0")) {
			return fmt.Errorf("%s: %q breaks the documented rules for phone numbers", number, combined)
		}
		if !hasSplit {
			c.fields[number+"CountryCode"], c.fields[number+"AreaCode"], c.fields[number+"Subscriber"] = m[1], m[2], m[3]
		}
	case hasSplit:
		c.fields[number] = countryCode + " " + c.fields[number+"AreaCode"] + " " + c.fields[number+"Subscriber"]
	}
	return nil
}

// applyDefaults sets what the sandbox fills in for a contact written without it.
func (c *fakeContact) applyDefaults() {
	for _, d := range [][2]string{
		{"salutation", "NO_SALUTATION"}, {"localPartnerContact", "false"},
		{"businessCardContact", "false"}, {"showOnProfilePage", "false"},
	} {
		if _, ok := c.fields[d[0]]; !ok {
			c.fields[d[0]] = d[1]
		}
	}
}

// renderContact is the GET body of a contact, formatted like the sandbox's.
// The caller holds f.mu.
func (f *fakeAPI) renderContact(id string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<common:realtorContactDetail ` + observedNamespaces + ` id="` + id + `">` + "\n")
	f.writeContactElements(&b, id, "    ")
	b.WriteString("</common:realtorContactDetail>\n")
	return b.String()
}

// renderContactList is the GET body of the contact list. The caller holds f.mu.
func (f *fakeAPI) renderContactList() string {
	var ids []string
	for id := range f.contacts.contacts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<common:realtorContactDetailsList ` + observedNamespaces + `>` + "\n")
	for _, id := range ids {
		b.WriteString(`    <realtorContactDetails id="` + id + `">` + "\n")
		f.writeContactElements(&b, id, "        ")
		b.WriteString("    </realtorContactDetails>\n")
	}
	b.WriteString("</common:realtorContactDetailsList>\n")
	return b.String()
}

// writeContactElements writes the elements of a contact in XSD order, one per
// line. The caller holds f.mu.
func (f *fakeAPI) writeContactElements(b *strings.Builder, id, indent string) {
	c := f.contacts.contacts[id]
	line := func(indent, name, text string) {
		b.WriteString(indent + "<" + name + ">")
		_ = xml.EscapeText(b, []byte(text))
		b.WriteString("</" + name + ">\n")
	}
	for _, name := range contactOrder {
		switch name {
		case "address":
			if c.address != nil {
				b.WriteString(indent + "<address>\n")
				for _, part := range c.address.Children {
					line(indent+"    ", part.Name, part.Text)
				}
				b.WriteString(indent + "</address>\n")
			}
		case "defaultContact":
			line(indent, name, strconv.FormatBool(id == f.contacts.defaultID))
		case "realEstateReferenceCount":
			count := 0
			for _, contactID := range f.contacts.listings {
				if contactID == id {
					count++
				}
			}
			line(indent, name, strconv.Itoa(count))
		default:
			if v, ok := c.fields[name]; ok {
				line(indent, name, v)
			}
		}
	}
}

// fakeContactCreatedBody is the sandbox's 201 body for a contact POST.
func fakeContactCreatedBody(id string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages ` + observedNamespaces + `>
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [contact] with id [` + id + `] has been created.</message>
        <id>` + id + `</id>
    </message>
</common:messages>
`
}
