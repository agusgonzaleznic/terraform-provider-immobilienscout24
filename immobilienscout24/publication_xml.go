package immobilienscout24

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// Namespaces of the common:* root elements, such as common:messages and
// common:publishObject, and of xlink, which the documented request declares.
const (
	commonNamespace = "http://rest.immobilienscout24.de/schema/common/1.0"
	xlinkNamespace  = "http://www.w3.org/1999/xlink"
)

// Publication is one real estate published on one publish channel.
type Publication struct {
	ID           string
	RealEstateID string
	ChannelID    string
}

// publicationID is the id the API gives the publication of a real estate on a
// channel.
func publicationID(realEstateID, channelID string) string {
	return realEstateID + "_" + channelID
}

// idElement is an element that carries only an id attribute, such as
// <realEstate id="..."/>.
type idElement struct {
	ID string `xml:"id,attr"`
}

// publishRequest serialises as the documented body of POST /offer/v1.0/publish.
// common:PublishObject is a sequence of realEstate and publishChannel, both
// unqualified, and the API rejects elements out of order.
type publishRequest struct {
	XMLName        xml.Name  `xml:"common:publishObject"`
	XMLNSCommon    string    `xml:"xmlns:common,attr"`
	XMLNSXlink     string    `xml:"xmlns:xlink,attr"`
	RealEstate     idElement `xml:"realEstate"`
	PublishChannel idElement `xml:"publishChannel"`
}

// publicationResponse decodes any root element so that a document of another
// type can be reported instead of silently misread.
type publicationResponse struct {
	XMLName        xml.Name
	ID             string    `xml:"id,attr"`
	RealEstate     idElement `xml:"realEstate"`
	PublishChannel idElement `xml:"publishChannel"`
}

func marshalPublishRequest(realEstateID, channelID string) ([]byte, error) {
	out, err := xml.Marshal(publishRequest{
		XMLNSCommon:    commonNamespace,
		XMLNSXlink:     xlinkNamespace,
		RealEstate:     idElement{ID: realEstateID},
		PublishChannel: idElement{ID: channelID},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding publishObject: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

// unmarshalPublication decodes the response to GET /offer/v1.0/publish/{id}
// and checks that it describes the publication that was asked for.
func unmarshalPublication(id string, body []byte) (*Publication, error) {
	var resp publicationResponse
	if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding publication response: %s", errorText(err.Error()))
	}
	if resp.XMLName.Space != commonNamespace || resp.XMLName.Local != "publishObject" {
		return nil, fmt.Errorf("expected a common:publishObject, the API returned <%s> in namespace %q",
			resp.XMLName.Local, resp.XMLName.Space)
	}
	p := &Publication{ID: id, RealEstateID: resp.RealEstate.ID, ChannelID: resp.PublishChannel.ID}
	if publicationID(p.RealEstateID, p.ChannelID) != id || (resp.ID != "" && resp.ID != id) {
		return nil, fmt.Errorf("asked for publication %s, the API returned publication %q of real estate %q on channel %q",
			id, resp.ID, p.RealEstateID, p.ChannelID)
	}
	return p, nil
}
