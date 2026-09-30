package immobilienscout24

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// xsiNamespace is the namespace of the xsi:type attribute, which names the
// type of an attachment.
const xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"

// Attachment types: the local part of xsi:type, whose prefix is common.
const (
	attachmentPicture = "Picture"
	attachmentPDF     = "PDFDocument"
	attachmentLink    = "Link"
)

// attachmentFields holds the elements of common:Attachment and of its
// subtypes that this provider manages. Each type is one xs:sequence per
// inheritance level, and the sandbox rejects elements out of order and
// requires floorplan and titlePicture (observed 2026-09-30), so the field
// order here IS the wire order. It serves all three types, because each one
// uses a subsequence of it:
//
//	Picture:     title, externalId, externalCheckSum, floorplan, titlePicture
//	PDFDocument: title, externalId, externalCheckSum, url, floorplan
//	Link:        title, externalId, externalCheckSum, url
//
// externalCheckSum follows externalId as in IS24's per-file common-1.0.xsd
// and the sandbox's schema errors; the XSD bundled with the WADL (testdata/)
// is older and lacks it. The server-owned checkSum and a picture's urls are
// ignored. Child elements are unqualified.
type attachmentFields struct {
	Title            string `xml:"title,omitempty"`
	ExternalID       string `xml:"externalId,omitempty"`
	ExternalCheckSum string `xml:"externalCheckSum,omitempty"`
	// URL is the target of a link. The API sets the one of a PDF document.
	URL          string `xml:"url,omitempty"`
	Floorplan    *bool  `xml:"floorplan"`
	TitlePicture *bool  `xml:"titlePicture"`
}

// attachmentDocument is what the provider reads and writes. The id is an
// attribute of the root in GET responses; it is never sent.
type attachmentDocument struct {
	ID string
	// Type is the local part of xsi:type, such as attachmentPicture.
	Type string
	attachmentFields
}

// attachmentRequest serialises as the documented request root,
// <common:attachment xsi:type="common:Picture" xmlns:common="..."
// xmlns:xlink="..." xmlns:xsi="...">, with the literal-prefix technique of
// apartmentRentRequest.
type attachmentRequest struct {
	XMLName     xml.Name `xml:"common:attachment"`
	Type        string   `xml:"xsi:type,attr"`
	XMLNSCommon string   `xml:"xmlns:common,attr"`
	XMLNSXlink  string   `xml:"xmlns:xlink,attr"`
	XMLNSXSI    string   `xml:"xmlns:xsi,attr"`
	attachmentFields
}

// attachmentResponse decodes any root element so that a document of another
// type can be reported instead of silently misread. Entries holds the
// attachments of a common:attachments list: GET .../attachment answers one,
// and so does one of the documented examples for a single attachment.
type attachmentResponse struct {
	XMLName xml.Name
	Type    string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	ID      string `xml:"id,attr"`
	attachmentFields
	Entries []attachmentResponse `xml:"attachment"`
}

func (r *attachmentResponse) document() *attachmentDocument {
	return &attachmentDocument{ID: r.ID, Type: r.Type[strings.IndexByte(r.Type, ':')+1:], attachmentFields: r.attachmentFields}
}

func marshalAttachment(doc *attachmentDocument) ([]byte, error) {
	out, err := xml.Marshal(attachmentRequest{
		Type:             "common:" + doc.Type,
		XMLNSCommon:      commonNamespace,
		XMLNSXlink:       xlinkNamespace,
		XMLNSXSI:         xsiNamespace,
		attachmentFields: doc.attachmentFields,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding attachment: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

// unmarshalAttachment decodes the response to GET .../attachment/{id} and
// checks that it describes the attachment that was asked for.
func unmarshalAttachment(id string, body []byte) (*attachmentDocument, error) {
	var resp attachmentResponse
	if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding attachment response: %w", err)
	}
	switch {
	case resp.XMLName.Space == commonNamespace && resp.XMLName.Local == "attachments":
		for i := range resp.Entries {
			if resp.Entries[i].ID == id {
				return resp.Entries[i].document(), nil
			}
		}
		return nil, fmt.Errorf("asked for attachment %s, the API returned a list without it", id)
	case resp.XMLName.Space != commonNamespace || resp.XMLName.Local != "attachment":
		return nil, fmt.Errorf("expected a common:attachment, the API returned <%s> in namespace %q",
			resp.XMLName.Local, resp.XMLName.Space)
	case resp.ID != "" && resp.ID != id:
		return nil, fmt.Errorf("asked for attachment %s, the API returned attachment %s", id, resp.ID)
	}
	return resp.document(), nil
}
