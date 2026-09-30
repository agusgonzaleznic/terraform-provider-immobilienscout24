package immobilienscout24

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// contactFields holds the elements of common:RealtorContactDetails that this
// provider manages. The type is one xs:sequence per inheritance level
// (BaseContactDetails, ContactDetails, RealtorContactDetails), so the field
// order here IS the wire order; the comments give each element's position in
// the flattened sequence. That sequence is the one of IS24's per-file
// common-1.0.xsd, which matches the documented maximal example. The XSD
// bundled with the WADL (testdata/) is older: it lacks position,
// secondaryEmail and showOnProfilePage. Child elements are unqualified.
//
// Phone numbers use the combined form, such as "+49 30 24301999". The API
// also accepts and returns them split into country code, area code and
// subscriber number (observed 2026-09-29); the split elements are ignored.
// The address is a common:Address, which is the start of a real estate's
// common:Wgs84Address, so addressElement serves both; a contact never
// carries coordinates.
type contactFields struct {
	Email             string          `xml:"email"`                     // 1  BaseContactDetails
	Salutation        string          `xml:"salutation,omitempty"`      // 2
	Firstname         string          `xml:"firstname,omitempty"`       // 3
	Lastname          string          `xml:"lastname"`                  // 4
	FaxNumber         string          `xml:"faxNumber,omitempty"`       // 8
	PhoneNumber       string          `xml:"phoneNumber,omitempty"`     // 12
	CellPhoneNumber   string          `xml:"cellPhoneNumber,omitempty"` // 16
	Address           *addressElement `xml:"address"`                   // 17
	CountryCode       string          `xml:"countryCode,omitempty"`     // 18
	Title             string          `xml:"title,omitempty"`           // 19
	AdditionName      string          `xml:"additionName,omitempty"`    // 20
	HomepageURL       string          `xml:"homepageUrl,omitempty"`     // 22
	Position          string          `xml:"position,omitempty"`        // 24
	SecondaryEmail    string          `xml:"secondaryEmail,omitempty"`  // 25
	DefaultContact    *bool           `xml:"defaultContact"`            // 28 RealtorContactDetails
	ExternalID        string          `xml:"externalId,omitempty"`      // 32
	ShowOnProfilePage *bool           `xml:"showOnProfilePage"`         // 33
}

// contactDocument is what the provider reads and writes. The id is an
// attribute of the root in GET responses; it is never sent.
type contactDocument struct {
	ID string
	contactFields
}

// contactRequest serialises as the documented request root,
// <common:realtorContactDetail xmlns:common="..." xmlns:xlink="...">, with the
// literal-prefix technique of marshalListing.
type contactRequest struct {
	XMLName     xml.Name `xml:"common:realtorContactDetail"`
	XMLNSCommon string   `xml:"xmlns:common,attr"`
	XMLNSXlink  string   `xml:"xmlns:xlink,attr"`
	contactFields
}

// contactResponse decodes any root element so that a document of another
// type can be reported instead of silently misread.
type contactResponse struct {
	XMLName xml.Name
	ID      string `xml:"id,attr"`
	contactFields
}

func marshalContact(doc *contactDocument) ([]byte, error) {
	out, err := xml.Marshal(contactRequest{
		XMLNSCommon:   commonNamespace,
		XMLNSXlink:    xlinkNamespace,
		contactFields: doc.contactFields,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding realtorContactDetail: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

// unmarshalContact decodes the response to GET /contact/{id} and checks that
// it describes the contact that was asked for.
func unmarshalContact(id string, body []byte) (*contactDocument, error) {
	var resp contactResponse
	if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding contact response: %w", err)
	}
	if resp.XMLName.Space != commonNamespace || resp.XMLName.Local != "realtorContactDetail" {
		return nil, fmt.Errorf("expected a common:realtorContactDetail, the API returned <%s> in namespace %q",
			resp.XMLName.Local, resp.XMLName.Space)
	}
	if resp.ID != "" && resp.ID != id {
		return nil, fmt.Errorf("asked for contact %s, the API returned contact %s", id, resp.ID)
	}
	return &contactDocument{ID: resp.ID, contactFields: resp.contactFields}, nil
}
