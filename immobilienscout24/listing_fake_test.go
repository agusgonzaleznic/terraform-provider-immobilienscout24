package immobilienscout24

// Tests of the listings on the fake API. Every sandbox fact that the fake
// reproduces (see fake_api_listing_test.go and fake_api_energy_test.go) has a
// test here that fails when the fake stops reproducing it.

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
)

// fakeListings is a client of a new fake API that writes listing bodies as
// they are, with or without the query parameter for the newer energy sources.
type fakeListings struct {
	t *testing.T
	f *fakeAPI
	c *Client
}

func newFakeListings(t *testing.T) *fakeListings {
	t.Helper()
	f := newFakeAPI(t)
	return &fakeListings{t: t, f: f, c: fakeClient(f)}
}

func query(newSources bool) string {
	if newSources {
		return realEstateQuery
	}
	return ""
}

// write creates a listing when id is empty and replaces it otherwise, and
// returns its id.
func (l *fakeListings) write(id string, body []byte, newSources bool) (string, error) {
	if id != "" {
		_, err := l.c.do(context.Background(), http.MethodPut, realEstatePath+url.PathEscape(id)+query(newSources), body)
		return id, err
	}
	resp, err := l.c.do(context.Background(), http.MethodPost, realEstatePath+query(newSources), body)
	if err != nil {
		return "", err
	}
	created, detail := createdID(resp, scoutID)
	if created == "" {
		l.t.Fatalf("the fake created a listing without an id: %s", detail)
	}
	return created, nil
}

// mustWrite is write for a body the fake must take.
func (l *fakeListings) mustWrite(id string, body []byte) string {
	l.t.Helper()
	id, err := l.write(id, body, true)
	if err != nil {
		l.t.Fatalf("the fake refused:\n%s\n%v", body, err)
	}
	return id
}

// get returns the GET response of a listing.
func (l *fakeListings) get(id string, newSources bool) *xnode {
	l.t.Helper()
	resp, err := l.c.do(context.Background(), http.MethodGet, realEstatePath+url.PathEscape(id)+query(newSources), nil)
	if err != nil {
		l.t.Fatal(err)
	}
	return parseResponse(l.t, resp.body)
}

// parseResponse parses a response body into a tree of elements by local name,
// with the texts trimmed.
func parseResponse(t *testing.T, body []byte) *xnode {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(body))
	var root *xnode
	var stack []*xnode
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("malformed XML: %v\n%s", err, body)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			n := &xnode{Name: tok.Name.Local}
			if len(stack) == 0 {
				root = n
			} else {
				stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(tok)
			}
		}
	}
	trimTexts(root)
	return root
}

// leaves lists the elements without children as path=text, in document
// order, without skip and what is inside it.
func leaves(n *xnode, prefix string, skip ...string) []string {
	var out []string
	for _, c := range n.Children {
		path := prefix + c.Name
		switch {
		case slices.Contains(skip, path):
		case len(c.Children) == 0:
			out = append(out, path+"="+c.Text)
		default:
			out = append(out, leaves(c, path+"/", skip...)...)
		}
	}
	return out
}

// texts returns the texts at a path such as
// "energySourcesEnev2014/energySourceEnev2014", in document order.
func texts(n *xnode, path string) []string {
	var out []string
	for _, l := range leaves(n, "") {
		if value, ok := strings.CutPrefix(l, path+"="); ok {
			out = append(out, value)
		}
	}
	return out
}

func sandboxFile(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/sandbox/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestFakeAnswersLikeTheSandbox replays requests the probe sent to the sandbox
// on 2026-09-30 and compares the fake's GET with the sandbox's, apart from the
// ids, times, geo hierarchy and links.
func TestFakeAnswersLikeTheSandbox(t *testing.T) {
	for name, requests := range map[string][]string{
		"apartmentBuy": {"apartmentBuy.post.xml"},
		"houseRent":    {"houseRent.post.xml"},
		"houseBuy":     {"houseBuy.post.xml"},
		// The probe then replaced the apartment with an energy certificate.
		"apartmentBuy-energy": {"apartmentBuy.post.xml", "apartmentBuy-energy.put.xml"},
	} {
		t.Run(name, func(t *testing.T) {
			l := newFakeListings(t)
			id := ""
			for _, file := range requests {
				id = l.mustWrite(id, sandboxFile(t, file))
			}
			skip := []string{"externalId", "creationDate", "lastModificationDate", "address/geoHierarchy",
				"realEstateState", "attachments", "contact", "publishChannels"}
			got := leaves(l.get(id, true), "", skip...)
			want := leaves(parseResponse(t, sandboxFile(t, name+".get.xml")), "", skip...)
			if !slices.Equal(got, want) {
				t.Fatalf("the fake answers\n  %s\nthe sandbox answered\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
		})
	}
}

// minimalListingFields are the elements every listing type requires.
func minimalListingFields() listingFields {
	show := true
	return listingFields{Title: "anonymized", ShowAddress: &show, Address: &addressElement{
		Street: "Invalidenstrasse", HouseNumber: "65", Postcode: "10557", City: "Berlin",
	}}
}

// listingBody is the body of a listing of the type root with the elements it
// requires, the prefix f and the energy and building elements b.
func listingBody(t *testing.T, root string, f listingFields, b buildingFields) []byte {
	t.Helper()
	s := func(v string) *string { return &v }
	courtage := &courtageElement{HasCourtage: "NO"}
	price := &priceElement{Value: s("99000"), Currency: "EUR"}
	var typ *realEstateType
	var doc any
	switch root {
	case "apartmentRent":
		typ, doc = &apartmentRentKind.realEstateType, &apartmentRentDocument{listingFields: f, buildingFields: b,
			BaseRent: s("521.22"), LivingSpace: s("72"), NumberOfRooms: s("3"), Courtage: courtage}
	case "apartmentBuy":
		typ, doc = &apartmentBuyKind.realEstateType, &apartmentBuyDocument{listingFields: f, buildingFields: b,
			Price: price, LivingSpace: s("50"), NumberOfRooms: s("2"), Courtage: courtage}
	case "houseRent":
		typ, doc = &houseRentKind.realEstateType, &houseRentDocument{listingFields: f, buildingFields: b,
			LivingSpace: s("120"), PlotArea: s("300"), NumberOfRooms: s("5"), Courtage: courtage,
			BuildingType: "NO_INFORMATION", BaseRent: s("1800")}
	case "houseBuy":
		typ, doc = &houseBuyKind.realEstateType, &houseBuyDocument{listingFields: f, buildingFields: b,
			BuildingType: "NO_INFORMATION", Price: price, LivingSpace: s("160"), PlotArea: s("450"),
			NumberOfRooms: s("6"), Courtage: courtage}
	default:
		t.Fatalf("no listing type %s", root)
	}
	body, err := marshalListing(typ, doc)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// The apartment rental of the sandbox, which the replay does not cover:
// what a create leaves out comes back filled in, constructionYearUnknown only
// while there is no constructionYear.
func TestFakeApartmentRentDefaults(t *testing.T) {
	l := newFakeListings(t)
	got := leaves(l.get(l.mustWrite("", listingBody(t, "apartmentRent", minimalListingFields(), buildingFields{})), true), "")
	for _, want := range []string{
		"apartmentType=NO_INFORMATION", "petsAllowed=NO_INFORMATION", "condition=NO_INFORMATION",
		"lift=false", "balcony=false", "garden=false", "builtInKitchen=false", "constructionYearUnknown=false",
		"certificateOfEligibilityNeeded=false", "cellar=NOT_APPLICABLE", "handicappedAccessible=NOT_APPLICABLE",
		"guestToilet=NOT_APPLICABLE", "useAsFlatshareRoom=NOT_APPLICABLE", "heatingCostsInServiceCharge=NOT_APPLICABLE",
		"energyConsumptionContainsWarmWater=NOT_APPLICABLE", "firingTypes/firingType=NO_INFORMATION",
		"energySourcesEnev2014/energySourceEnev2014=NO_INFORMATION",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("GET lacks %s:\n%s", want, strings.Join(got, "\n"))
		}
	}
	year := "1990"
	withYear := l.get(l.mustWrite("", listingBody(t, "apartmentRent", minimalListingFields(), buildingFields{ConstructionYear: &year})), true)
	if texts(withYear, "constructionYearUnknown") != nil {
		t.Error("constructionYearUnknown came back although constructionYear is set")
	}
}

// Every listing type: the address is geocoded, externalId defaults to the id,
// and a PUT replaces the whole listing, the contact included.
func TestFakeListingsReplaceAndFillIn(t *testing.T) {
	for _, root := range fakeListingRoots {
		t.Run(root, func(t *testing.T) {
			l := newFakeListings(t)
			contact := l.f.AddContactOutOfBand("tf-acc-website@is24-test.de", "")
			f := minimalListingFields()
			f.DescriptionNote, f.Contact = "anonymized", &idElement{ID: contact}
			id := l.mustWrite("", listingBody(t, root, f, buildingFields{}))
			got := l.get(id, true)
			if texts(got, "externalId")[0] != id || texts(got, "address/wgs84Coordinate/latitude") == nil ||
				texts(got, "descriptionNote") == nil || l.f.ListingContact(id) != contact || got.Name != root {
				t.Fatalf("after the create:\n%s", strings.Join(leaves(got, ""), "\n"))
			}
			l.mustWrite(id, listingBody(t, root, minimalListingFields(), buildingFields{}))
			if got := l.get(id, true); texts(got, "descriptionNote") != nil || l.f.ListingContact(id) != fakeDefaultContactID {
				t.Fatalf("a PUT without descriptionNote and contact kept them: contact %s\n%s", l.f.ListingContact(id),
					strings.Join(leaves(got, ""), "\n"))
			}
		})
	}
}

// Decimals come back rounded half up to two places, exactly on the decimal:
// livingSpace 50.555, a float64 of 50.55499..., is 50.56. xs:double ones come
// back with two decimals, thermalCharacteristic and numberOfRooms without
// trailing zeros, and a houseBuy without a price has one of 0.00. Every value
// here was observed on the sandbox.
func TestFakeRoundsDecimals(t *testing.T) {
	l := newFakeListings(t)
	s := func(v string) *string { return &v }
	courtage := &courtageElement{HasCourtage: "NO"}
	for name, tc := range map[string]struct {
		typ  *realEstateType
		doc  any
		want []string
	}{
		"apartment for rent": {&apartmentRentKind.realEstateType, &apartmentRentDocument{listingFields: minimalListingFields(),
			buildingFields: buildingFields{ThermalCharacteristic: s("95.555")}, BaseRent: s("521.22"),
			LivingSpace: s("50.555"), NumberOfRooms: s("3"), Courtage: courtage},
			[]string{"thermalCharacteristic=95.56", "livingSpace=50.56", "baseRent=521.22"}},
		"apartment for sale": {&apartmentBuyKind.realEstateType, &apartmentBuyDocument{listingFields: minimalListingFields(),
			buildingFields: buildingFields{ThermalCharacteristic: s("25")}, Price: &priceElement{Value: s("99000"), Currency: "EUR"},
			LivingSpace: s("50"), NumberOfRooms: s("2"), Courtage: courtage},
			[]string{"thermalCharacteristic=25", "price/value=99000.00", "livingSpace=50.00", "numberOfRooms=2"}},
		"house for rent": {&houseRentKind.realEstateType, &houseRentDocument{listingFields: minimalListingFields(),
			buildingFields: buildingFields{ThermalCharacteristic: s("95.5")}, LivingSpace: s("180.27"), PlotArea: s("125.717"),
			NumberOfRooms: s("22"), Courtage: courtage, BuildingType: "MULTI_FAMILY_HOUSE", BaseRent: s("986.17")},
			[]string{"thermalCharacteristic=95.5", "livingSpace=180.27", "plotArea=125.72", "numberOfRooms=22", "baseRent=986.17"}},
		"house for sale without a price": {&houseBuyKind.realEstateType, &houseBuyDocument{listingFields: minimalListingFields(),
			BuildingType: "SEMIDETACHED_HOUSE", LivingSpace: s("160"), PlotArea: s("450"), NumberOfRooms: s("6"), Courtage: courtage},
			[]string{"price/value=0.00", "price/currency=EUR", "price/marketingType=PURCHASE", "price/priceIntervalType=ONE_TIME_CHARGE"}},
	} {
		body, err := marshalListing(tc.typ, tc.doc)
		if err != nil {
			t.Fatal(err)
		}
		got := leaves(l.get(l.mustWrite("", body), true), "")
		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Errorf("%s: GET lacks %s:\n%s", name, want, strings.Join(got, "\n"))
			}
		}
	}
	// An explicit price of 0 was taken on an apartmentBuy POST and a houseBuy
	// PUT, and came back as 0.00.
	id := l.mustWrite("", listingBody(t, "houseBuy", minimalListingFields(), buildingFields{}))
	for _, write := range []struct{ root, id string }{{"apartmentBuy", ""}, {"houseBuy", id}} {
		body := bytes.Replace(listingBody(t, write.root, minimalListingFields(), buildingFields{}),
			[]byte("<value>99000</value>"), []byte("<value>0</value>"), 1)
		if got := texts(l.get(l.mustWrite(write.id, body), true), "price/value"); !slices.Equal(got, []string{"0.00"}) {
			t.Errorf("%s with a price of 0: price/value %v", write.root, got)
		}
	}
}
