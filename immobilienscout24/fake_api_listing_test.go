package immobilienscout24

// The listing types of the fake API in fake_api_test.go. As the live sandbox
// did on 2026-09-29 and 2026-09-30, the fake takes the four listing roots,
// checks a body against the sequence of its type in the XSD fixture and the
// values of enumerated elements against their simple types, and:
//
//   - fills in what a write leaves out, the defaults in fakeListingDefaults;
//     constructionYearUnknown false only while no constructionYear is set;
//   - adds marketingType PURCHASE and priceIntervalType ONE_TIME_CHARGE to the
//     price of a sale, and gives a houseBuy without a price one of 0.00;
//   - rounds decimals half up to two places: an xs:double renders with two
//     decimals (72.00), thermalCharacteristic and numberOfRooms without
//     trailing zeros (25, 95.5, 2);
//   - handles the energy fields as fake_api_energy_test.go describes.
//
// TestFakeAnswersLikeTheSandbox replays requests the probe sent to the sandbox
// and compares the fake's GET with the sandbox's. Simulated, not observed: the
// rounding of numberOfRooms; that a PUT cannot change the type of a listing;
// the texts of the schema errors.

import (
	"encoding/xml"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"testing"
)

// fakeListingRoots are the root elements of the listing types.
var fakeListingRoots = []string{"apartmentRent", "apartmentBuy", "houseRent", "houseBuy"}

// fakeListingType is one listing type of the fake API.
type fakeListingType struct {
	order []xsdElement      // the flattened sequence of the type
	index map[string]int    // the position of each element in order
	sale  bool              // whether it is a type for sale, with a price
	xsd   map[string]string // the XSD type of each element, such as xs:double
}

// fakeListingSchema is what the fake checks listings against. It never changes.
type fakeListingSchema struct {
	types                         map[string]*fakeListingType // by root element
	address, price, energySources []xsdElement
	// certificate is common:EnergyPerformanceCertificate, an xs:all.
	certificate []xsdElement
	xsd         *xsdSchemas
}

func newFakeListingSchema(t testing.TB) fakeListingSchema {
	s := fakeListingSchema{
		types:         map[string]*fakeListingType{},
		address:       mustElements(t, commonNamespace, "Wgs84Address"),
		price:         mustElements(t, commonNamespace, "Price"),
		energySources: mustElements(t, commonNamespace, "EnergySourcesEnev2014"),
		certificate:   mustElements(t, commonNamespace, "EnergyPerformanceCertificate"),
		xsd:           liveXSD(t),
	}
	for _, root := range fakeListingRoots {
		typ := &fakeListingType{
			order: mustElements(t, realEstatesNamespace, strings.ToUpper(root[:1])+root[1:]),
			index: map[string]int{}, xsd: map[string]string{}, sale: strings.HasSuffix(root, "Buy"),
		}
		for i, e := range typ.order {
			typ.index[e.Name], typ.xsd[e.Name] = i, e.Type
		}
		s.types[root] = typ
	}
	return s
}

// fakeListingDefaults are the values the sandbox sets for fields that a write
// leaves out, by listing type. Every type also gets one NO_INFORMATION entry in
// firingTypes and in energySourcesEnev2014, see completeEnergy.
var fakeListingDefaults = map[string][][2]string{
	"apartmentRent": {
		{"apartmentType", "NO_INFORMATION"}, {"lift", "false"}, {"cellar", "NOT_APPLICABLE"},
		{"handicappedAccessible", "NOT_APPLICABLE"}, {"condition", "NO_INFORMATION"},
		{"constructionYearUnknown", "false"}, {"energyConsumptionContainsWarmWater", "NOT_APPLICABLE"},
		{"guestToilet", "NOT_APPLICABLE"}, {"heatingCostsInServiceCharge", "NOT_APPLICABLE"},
		{"petsAllowed", "NO_INFORMATION"}, {"useAsFlatshareRoom", "NOT_APPLICABLE"}, {"builtInKitchen", "false"},
		{"balcony", "false"}, {"certificateOfEligibilityNeeded", "false"}, {"garden", "false"},
	},
	"apartmentBuy": {
		{"apartmentType", "NO_INFORMATION"}, {"lift", "false"}, {"cellar", "NOT_APPLICABLE"},
		{"handicappedAccessible", "NOT_APPLICABLE"}, {"condition", "NO_INFORMATION"},
		{"constructionYearUnknown", "false"}, {"energyConsumptionContainsWarmWater", "NOT_APPLICABLE"},
		{"guestToilet", "NOT_APPLICABLE"}, {"rented", "NOT_APPLICABLE"}, {"listed", "NOT_APPLICABLE"},
		{"summerResidencePractical", "NOT_APPLICABLE"}, {"builtInKitchen", "false"}, {"balcony", "false"},
		{"garden", "false"},
	},
	"houseRent": {
		{"cellar", "NOT_APPLICABLE"}, {"handicappedAccessible", "NOT_APPLICABLE"}, {"condition", "NO_INFORMATION"},
		{"constructionYearUnknown", "false"}, {"energyConsumptionContainsWarmWater", "NOT_APPLICABLE"},
		{"guestToilet", "NOT_APPLICABLE"}, {"heatingCostsInServiceCharge", "NOT_APPLICABLE"},
		{"petsAllowed", "NO_INFORMATION"}, {"useAsFlatshareRoom", "NOT_APPLICABLE"}, {"builtInKitchen", "false"},
	},
	"houseBuy": {
		{"lodgerFlat", "NOT_APPLICABLE"}, {"constructionPhase", "NO_INFORMATION"}, {"cellar", "NOT_APPLICABLE"},
		{"handicappedAccessible", "NOT_APPLICABLE"}, {"condition", "NO_INFORMATION"},
		{"constructionYearUnknown", "false"}, {"energyConsumptionContainsWarmWater", "NOT_APPLICABLE"},
		{"guestToilet", "NOT_APPLICABLE"}, {"rented", "NOT_APPLICABLE"}, {"listed", "NOT_APPLICABLE"},
		{"summerResidencePractical", "NOT_APPLICABLE"},
	},
}

// validate parses a real estate request body and checks it against the XSD
// fixture and the sandbox's rules. The type of the listing is the name of the
// returned root.
func (f *fakeAPI) validate(body []byte) (*xnode, error) {
	root, err := parseRequest(body, realEstatesNamespace, fakeListingRoots...)
	if err != nil {
		return nil, err
	}
	trimTexts(root)
	typ := f.listing.types[root.Name]
	if err := checkOrder(root.Name, childNames(root), typ.order); err != nil {
		return nil, err
	}
	if a := root.child("address"); a != nil {
		if err := checkOrder("address", childNames(a), f.listing.address); err != nil {
			return nil, err
		}
	}
	if c := root.child("courtage"); c != nil && c.child("hasCourtage") == nil {
		return nil, fmt.Errorf("courtage: required element <hasCourtage> is missing")
	}
	for _, c := range root.Children {
		if err := f.listing.checkValue(root.Name, c, typ.order[typ.index[c.Name]]); err != nil {
			return nil, err
		}
	}
	if p := root.child("price"); p != nil {
		if err := checkOrder("price", childNames(p), f.listing.price); err != nil {
			return nil, err
		}
		for _, c := range p.Children {
			if err := f.listing.checkValue("price", c, elementNamed(f.listing.price, c.Name)); err != nil {
				return nil, err
			}
		}
	}
	if err := f.checkEnergy(root); err != nil {
		return nil, err
	}
	if err := f.checkListingContact(root); err != nil {
		return nil, err
	}
	return root, nil
}

// checkValue checks the value of an element whose type is an enumeration.
func (s *fakeListingSchema) checkValue(context string, n *xnode, e xsdElement) error {
	if values := s.xsd.enumeration(e.TypeName); values != nil && !slices.Contains(values, n.Text) {
		return fmt.Errorf("%s: <%s>%s</%s> is not a value of %s", context, n.Name, n.Text, n.Name, e.Type)
	}
	return nil
}

// elementNamed returns the element of a sequence with the name, or the zero
// element when there is none.
func elementNamed(els []xsdElement, name string) xsdElement {
	if i := slices.IndexFunc(els, func(e xsdElement) bool { return e.Name == name }); i >= 0 {
		return els[i]
	}
	return xsdElement{}
}

// trimTexts trims the text of an element and of everything inside it.
func trimTexts(n *xnode) {
	n.Text = strings.TrimSpace(n.Text)
	for _, c := range n.Children {
		trimTexts(c)
	}
}

// completeListing fills in what the sandbox fills in for a listing written
// without it, and puts the elements in XSD order. The caller holds f.mu.
func (f *fakeAPI) completeListing(obj *xnode) {
	typ := f.listing.types[obj.Name]
	for _, d := range fakeListingDefaults[obj.Name] {
		if d[0] == "constructionYearUnknown" && obj.child("constructionYear") != nil {
			continue
		}
		if obj.child(d[0]) == nil {
			obj.Children = append(obj.Children, &xnode{Name: d[0], Text: d[1]})
		}
	}
	if typ.sale {
		p := obj.child("price")
		if p == nil && obj.Name == "houseBuy" {
			p = &xnode{Name: "price", Children: []*xnode{{Name: "value", Text: "0"}, {Name: "currency", Text: "EUR"}}}
			obj.Children = append(obj.Children, p)
		}
		for _, d := range [][2]string{{"marketingType", "PURCHASE"}, {"priceIntervalType", "ONE_TIME_CHARGE"}} {
			if p != nil && p.child(d[0]) == nil {
				p.Children = append(p.Children, &xnode{Name: d[0], Text: d[1]})
			}
		}
	}
	completeEnergy(obj)
	sort.SliceStable(obj.Children, func(i, j int) bool {
		return typ.index[obj.Children[i].Name] < typ.index[obj.Children[j].Name]
	})
}

// render serialises an object the way a GET with the query parameter returns it.
func (f *fakeAPI) render(id string, obj *xnode) string {
	return f.renderListing(id, obj, true)
}

// renderListing serialises an object the way the Retrieve page shows it: the
// id as a root attribute, plus server-populated elements the client never
// sent. Without newSources it leaves energySourcesEnev2014 out, as a GET
// without the query parameter does.
func (f *fakeAPI) renderListing(id string, obj *xnode, newSources bool) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	typ := f.listing.types[obj.Name]
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<realestates:` + obj.Name + ` xmlns:ns2="http://rest.immobilienscout24.de/schema/platform/gis/1.0" ` +
		`xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" ` +
		`xmlns:realestates="` + realEstatesNamespace + `" id="` + id + `">`)
	for _, c := range obj.Children {
		if c.Name == "energySourcesEnev2014" && !newSources {
			continue
		}
		out := renderNumbers(typ, c)
		switch c.Name {
		case "address":
			out.Children = append(append([]*xnode{}, c.Children...), &xnode{Name: "geoHierarchy", Children: []*xnode{
				{Name: "city", Children: []*xnode{{Name: "geoCodeId", Text: "1"}}},
			}})
		case "showAddress":
			// The documented GET example carries attachments before showAddress.
			writeNode(&b, &xnode{Name: "attachments", Attrs: []xml.Attr{{
				Name:  xml.Name{Local: "xlink:href"},
				Value: f.BaseURL() + "/offer/v1.0/user/me/realestate/" + id + "/attachment",
			}}})
		}
		writeNode(&b, out)
		switch c.Name {
		case "title":
			writeNode(&b, &xnode{Name: "creationDate", Text: "2026-09-29T10:00:00.000+02:00"})
			writeNode(&b, &xnode{Name: "lastModificationDate", Text: "2026-09-29T10:00:00.000+02:00"})
		case "address":
			writeNode(&b, &xnode{Name: "realEstateState", Text: f.realEstateState(id)})
		case "showAddress":
			f.writeListingContact(&b, id)
			f.writePublishChannels(&b, id)
		}
	}
	b.WriteString(`</realestates:` + obj.Name + `>`)
	return b.String()
}

// renderNumbers returns an element with its decimals as the sandbox renders them.
func renderNumbers(typ *fakeListingType, c *xnode) *xnode {
	out := *c
	switch {
	case c.Name == "thermalCharacteristic" || c.Name == "numberOfRooms":
		// numberOfRooms 2 of an apartmentBuy came back as 2 although it is an
		// xs:double there; in the house types it is an xs:string.
		out.Text = fakeRound(c.Text, false)
	case typ.xsd[c.Name] == "xs:double":
		out.Text = fakeRound(c.Text, true)
	case c.Name == "price":
		out.Children = nil
		for _, p := range c.Children {
			value := *p
			if p.Name == "value" {
				value.Text = fakeRound(p.Text, true)
			}
			out.Children = append(out.Children, &value)
		}
	}
	return &out
}

// fakeRound rounds a decimal half up to two places, as the sandbox does
// (livingSpace 50.555 came back as 50.56): exactly, on the decimal, not on a
// float64, which holds 50.555 as 50.55499... pad keeps two decimals, as for
// an xs:double; otherwise trailing zeros go. Text that is not a number stays.
func fakeRound(text string, pad bool) string {
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		return text
	}
	cents := new(big.Rat).Mul(new(big.Rat).Abs(r), big.NewRat(100, 1))
	cents.Add(cents, big.NewRat(1, 2))
	whole := new(big.Int).Quo(cents.Num(), cents.Denom())
	if r.Sign() < 0 {
		whole.Neg(whole)
	}
	s := new(big.Rat).SetFrac(whole, big.NewInt(100)).FloatString(2)
	if !pad {
		s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	}
	return s
}
