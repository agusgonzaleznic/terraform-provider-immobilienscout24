package immobilienscout24

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// realEstatesNamespace is the namespace of the realestates:* root elements.
const realEstatesNamespace = "http://rest.immobilienscout24.de/schema/offer/realestates/1.0"

// realEstateType names a listing type by the local name of its XSD root
// element, such as apartmentRent, and says what a resource of the type
// manages, for the error about a listing of another type.
type realEstateType struct {
	root   string
	plural string
}

// listingFields holds the elements every listing type starts with: positions
// 1 to 19 of the flattened sequence, the same in every type. The live XSD
// defines each listing type as one xs:sequence per inheritance level
// (AbstractRealEstateForList, AbstractRealEstate, RealEstate, then the type
// itself) and the API rejects elements out of order, so the field order of a
// listing's document IS the wire order: it embeds listingFields first, then
// its own elements, with buildingFields at the position of energyCertificate.
// The comments give each element's position in the flattened sequence. Child
// elements are unqualified (elementFormDefault).
//
// Numbers are strings so that the provider controls their formatting, and
// optional values are pointers so that "absent" differs from "false" or "0".
type listingFields struct {
	ExternalID      string          `xml:"externalId,omitempty"`      // 1  AbstractRealEstateForList
	Title           string          `xml:"title"`                     // 2
	Address         *addressElement `xml:"address"`                   // 5
	DescriptionNote string          `xml:"descriptionNote,omitempty"` // 9  AbstractRealEstate
	FurnishingNote  string          `xml:"furnishingNote,omitempty"`  // 10
	LocationNote    string          `xml:"locationNote,omitempty"`    // 11
	OtherNote       string          `xml:"otherNote,omitempty"`       // 12
	ShowAddress     *bool           `xml:"showAddress"`               // 17 RealEstate
	Contact         *idElement      `xml:"contact"`                   // 18
}

// buildingFields holds the energy and building elements that every listing
// type has in the same order, from energyCertificate to numberOfFloors. The
// comments give the positions in ApartmentRent and ApartmentBuy; in HouseRent
// each element is 3 places later, in HouseBuy 1 place earlier.
type buildingFields struct {
	EnergyCertificate                  *energyCertificateElement `xml:"energyCertificate"`                            // 24
	Cellar                             string                    `xml:"cellar,omitempty"`                             // 25
	ConstructionYear                   *string                   `xml:"constructionYear"`                             // 32
	FreeFrom                           string                    `xml:"freeFrom,omitempty"`                           // 33
	HeatingTypeEnev2014                string                    `xml:"heatingTypeEnev2014,omitempty"`                // 35
	EnergySources                      *energySourcesElement     `xml:"energySourcesEnev2014"`                        // 37
	BuildingEnergyRatingType           string                    `xml:"buildingEnergyRatingType,omitempty"`           // 38
	ThermalCharacteristic              *string                   `xml:"thermalCharacteristic"`                        // 39
	EnergyConsumptionContainsWarmWater string                    `xml:"energyConsumptionContainsWarmWater,omitempty"` // 40
	NumberOfFloors                     *string                   `xml:"numberOfFloors"`                               // 41
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

// energyCertificateElement is common:EnergyPerformanceCertificate, an xs:all,
// so order is free. The API adds legalConstructionYear from constructionYear
// (observed 2026-09-30); it and the other children are not read.
type energyCertificateElement struct {
	Availability    string `xml:"energyCertificateAvailability,omitempty"`
	CreationDate    string `xml:"energyCertificateCreationDate,omitempty"`
	EfficiencyClass string `xml:"energyEfficiencyClass,omitempty"`
}

// energySourcesElement is common:EnergySourcesEnev2014, a sequence of
// energySourceEnev2014.
type energySourcesElement struct {
	Sources []string `xml:"energySourceEnev2014"`
}

// priceElement is common:Price, the sequence value, currency, marketingType,
// priceIntervalType. A sale sends value and currency; the API adds the other
// two, PURCHASE and ONE_TIME_CHARGE (observed 2026-09-30), which are not read.
type priceElement struct {
	Value    *string `xml:"value"`
	Currency string  `xml:"currency,omitempty"`
}

// value returns the value of the price, or nil when there is no price.
func (e *priceElement) value() *string {
	if e == nil {
		return nil
	}
	return e.Value
}

// marshalListing serialises doc, the document of a listing type, as the
// documented request root <realestates:{root} xmlns:realestates="...">.
// encoding/xml would otherwise declare the namespace as the default
// namespace, which would put every child element into it and break the
// unqualified element form.
func marshalListing(typ *realEstateType, doc any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	start := xml.StartElement{
		Name: xml.Name{Local: "realestates:" + typ.root},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns:realestates"}, Value: realEstatesNamespace}},
	}
	if err := xml.NewEncoder(&b).EncodeElement(doc, start); err != nil {
		return nil, fmt.Errorf("encoding %s: %w", typ.root, err)
	}
	return b.Bytes(), nil
}

// unmarshalListing decodes a GET response into doc, the document of a
// listing type. A real estate of another type is reported instead of
// silently misread.
func unmarshalListing(typ *realEstateType, body []byte, doc any) error {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("decoding real estate response: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space != realEstatesNamespace || start.Name.Local != typ.root {
			return fmt.Errorf("expected a realestates:%s, the API returned <%s> in namespace %q; "+
				"this resource only manages %s", typ.root, start.Name.Local, start.Name.Space, typ.plural)
		}
		if err := dec.DecodeElement(doc, &start); err != nil {
			return fmt.Errorf("decoding real estate response: %w", err)
		}
		return nil
	}
}
