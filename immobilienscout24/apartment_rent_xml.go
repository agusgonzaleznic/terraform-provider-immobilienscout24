package immobilienscout24

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// realEstatesNamespace is the namespace of the realestates:* root elements.
const realEstatesNamespace = "http://rest.immobilienscout24.de/schema/offer/realestates/1.0"

// apartmentRentFields holds the elements of realestates:ApartmentRent that this
// provider manages. The live XSD defines ApartmentRent as one xs:sequence per
// inheritance level (AbstractRealEstateForList, AbstractRealEstate, RealEstate,
// ApartmentRent) and the API rejects elements out of order, so the field order
// here IS the wire order. The comments give each element's position in the
// flattened sequence. Child elements are unqualified (elementFormDefault).
//
// Numbers are strings so that the provider controls their formatting, and
// optional values are pointers so that "absent" differs from "false" or "0".
type apartmentRentFields struct {
	ExternalID                  string           `xml:"externalId,omitempty"`                  // 1  AbstractRealEstateForList
	Title                       string           `xml:"title"`                                 // 2
	Address                     *addressElement  `xml:"address"`                               // 5
	DescriptionNote             string           `xml:"descriptionNote,omitempty"`             // 9  AbstractRealEstate
	FurnishingNote              string           `xml:"furnishingNote,omitempty"`              // 10
	LocationNote                string           `xml:"locationNote,omitempty"`                // 11
	OtherNote                   string           `xml:"otherNote,omitempty"`                   // 12
	ShowAddress                 *bool            `xml:"showAddress"`                           // 17 RealEstate
	ApartmentType               string           `xml:"apartmentType,omitempty"`               // 20 ApartmentRent
	Floor                       *string          `xml:"floor"`                                 // 21
	Lift                        *bool            `xml:"lift"`                                  // 22
	Cellar                      string           `xml:"cellar,omitempty"`                      // 25
	FreeFrom                    string           `xml:"freeFrom,omitempty"`                    // 33
	NumberOfFloors              *string          `xml:"numberOfFloors"`                        // 41
	BaseRent                    *string          `xml:"baseRent"`                              // 47
	TotalRent                   *string          `xml:"totalRent"`                             // 48
	ServiceCharge               *string          `xml:"serviceCharge"`                         // 49
	Deposit                     string           `xml:"deposit,omitempty"`                     // 50
	HeatingCosts                *string          `xml:"heatingCosts"`                          // 51
	HeatingCostsInServiceCharge string           `xml:"heatingCostsInServiceCharge,omitempty"` // 52
	PetsAllowed                 string           `xml:"petsAllowed,omitempty"`                 // 53
	LivingSpace                 *string          `xml:"livingSpace"`                           // 57
	NumberOfRooms               *string          `xml:"numberOfRooms"`                         // 58
	BuiltInKitchen              *bool            `xml:"builtInKitchen"`                        // 60
	Balcony                     *bool            `xml:"balcony"`                               // 61
	Garden                      *bool            `xml:"garden"`                                // 63
	Courtage                    *courtageElement `xml:"courtage"`                              // 64
}

// addressElement is common:Wgs84Address: the common:Address sequence
// (street, houseNumber, postcode, city, internationalCountryRegion) followed by
// quarter, wgs84Coordinate, preciseHouseNumber, geoHierarchy, description.
type addressElement struct {
	Street          string             `xml:"street,omitempty"`
	HouseNumber     string             `xml:"houseNumber,omitempty"`
	Postcode        string             `xml:"postcode,omitempty"`
	City            string             `xml:"city,omitempty"`
	Wgs84Coordinate *coordinateElement `xml:"wgs84Coordinate"`
}

// coordinateElement is common:Wgs84Coordinate (latitude, longitude; both
// xs:string and both required).
type coordinateElement struct {
	Latitude  string `xml:"latitude"`
	Longitude string `xml:"longitude"`
}

// courtageElement is common:CourtageInfo, an xs:all, so order is free.
type courtageElement struct {
	HasCourtage  string `xml:"hasCourtage"`
	Courtage     string `xml:"courtage,omitempty"`
	CourtageNote string `xml:"courtageNote,omitempty"`
}

// apartmentRentDocument is what the provider reads and writes. The scout id
// is an attribute of the root in GET responses; it is never sent.
type apartmentRentDocument struct {
	ID string
	apartmentRentFields
}

// apartmentRentRequest serialises as the documented request root,
// <realestates:apartmentRent xmlns:realestates="...">. encoding/xml would
// otherwise declare the namespace as the default namespace, which would put
// every child element into it and break the unqualified element form.
type apartmentRentRequest struct {
	XMLName        xml.Name `xml:"realestates:apartmentRent"`
	XMLNSNamespace string   `xml:"xmlns:realestates,attr"`
	apartmentRentFields
}

// apartmentRentResponse decodes any root element so that a real estate of a
// different type can be reported instead of silently misread.
type apartmentRentResponse struct {
	XMLName xml.Name
	ID      string `xml:"id,attr"`
	apartmentRentFields
}

func marshalApartmentRent(doc *apartmentRentDocument) ([]byte, error) {
	out, err := xml.Marshal(apartmentRentRequest{
		XMLNSNamespace:      realEstatesNamespace,
		apartmentRentFields: doc.apartmentRentFields,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding apartmentRent: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

func unmarshalApartmentRent(body []byte) (*apartmentRentDocument, error) {
	var resp apartmentRentResponse
	if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding real estate response: %w", err)
	}
	if resp.XMLName.Space != realEstatesNamespace || resp.XMLName.Local != "apartmentRent" {
		return nil, fmt.Errorf("expected a realestates:apartmentRent, the API returned <%s> in namespace %q; "+
			"this resource only manages apartment rentals", resp.XMLName.Local, resp.XMLName.Space)
	}
	return &apartmentRentDocument{ID: resp.ID, apartmentRentFields: resp.apartmentRentFields}, nil
}
