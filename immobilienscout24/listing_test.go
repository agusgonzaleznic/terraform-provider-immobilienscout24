package immobilienscout24

// Unit tests of the code the listing resources share, and of the documents of
// the listing types besides apartment rent, whose own tests are in
// client_test.go.

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// fullListingModel sets every attribute that every listing type has.
func fullListingModel() listingModel {
	s, f := types.StringValue, types.Float64Value
	m := fullModel().listingModel
	m.EnergySources = types.SetValueMust(types.StringType, []attr.Value{s("GAS"), s("SOLAR_HEATING")})
	m.ThermalCharacteristic = f(80.25)
	return m
}

// fullApartmentBuy, fullHouseRent and fullHouseBuy set every attribute their
// resource models.
func fullApartmentBuy() *apartmentBuyModel {
	s, b, f := types.StringValue, types.BoolValue, types.Float64Value
	return &apartmentBuyModel{listingModel: fullListingModel(),
		ApartmentType: s("APARTMENT"), Floor: types.Int64Value(2), Lift: b(true), Rented: s("YES"),
		PurchasePrice: f(199000), LivingSpace: f(50.5), NumberOfRooms: f(2.5),
		BuiltInKitchen: b(true), Balcony: b(true), Garden: b(false), ServiceCharge: f(250.5),
	}
}

func fullHouseRent() *houseRentModel {
	s, f := types.StringValue, types.Float64Value
	return &houseRentModel{listingModel: fullListingModel(),
		LivingSpace: f(120), PlotArea: f(300.5), NumberOfRooms: f(5.5), BuildingType: s("SINGLE_FAMILY_HOUSE"),
		BaseRent: f(1800), TotalRent: f(2200), ServiceCharge: f(250), Deposit: s("3 Kaltmieten"), HeatingCosts: f(150),
		HeatingCostsInServiceCharge: s("NO"), PetsAllowed: s("NEGOTIABLE"), BuiltInKitchen: types.BoolValue(true),
	}
}

func fullHouseBuy() *houseBuyModel {
	s, f := types.StringValue, types.Float64Value
	return &houseBuyModel{listingModel: fullListingModel(),
		BuildingType: s("SEMIDETACHED_HOUSE"), Rented: s("YES"), PurchasePrice: f(650000),
		LivingSpace: f(160), PlotArea: f(450), NumberOfRooms: f(6),
	}
}

func TestListingDocumentsFollowXSD(t *testing.T) {
	for name, tc := range map[string]struct {
		typ     *realEstateType
		xsdType string
		doc     any
		count   int
	}{
		"apartmentBuy": {&apartmentBuyKind.realEstateType, "ApartmentBuy", fullApartmentBuy().toDocument(), 31},
		"houseRent":    {&houseRentKind.realEstateType, "HouseRent", fullHouseRent().toDocument(), 32},
		"houseBuy":     {&houseBuyKind.realEstateType, "HouseBuy", fullHouseBuy().toDocument(), 26},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := marshalListing(tc.typ, tc.doc)
			if err != nil {
				t.Fatal(err)
			}
			root, names := topLevelChildren(t, body)
			if root.Space != realEstatesNamespace || root.Local != name {
				t.Fatalf("root = %+v", root)
			}
			if !bytes.Contains(body, []byte(`<realestates:`+name+` xmlns:realestates="`+realEstatesNamespace+`">`)) {
				t.Fatalf("root is not in the documented prefixed form:\n%s", body)
			}
			if err := checkOrder(name, names, mustElements(t, realEstatesNamespace, tc.xsdType)); err != nil {
				t.Fatalf("%v\nsent: %v", err, names)
			}
			// Every modelled field must actually be on the wire.
			if len(names) != tc.count {
				t.Fatalf("full model produced %d elements, want %d: %v", len(names), tc.count, names)
			}
			if _, err := newFakeAPI(t).validate(body); err != nil {
				t.Fatalf("fake API rejects the body: %v", err)
			}
		})
	}
}

// The GET responses of the sandbox (testdata/sandbox) map onto the models,
// and what the API adds on its own is ignored.
func TestSandboxResponsesMapOntoTheModels(t *testing.T) {
	get := func(name string, typ *realEstateType, doc any) {
		t.Helper()
		if err := unmarshalListing(typ, sandboxFile(t, name), doc); err != nil {
			t.Fatal(err)
		}
	}
	energy := &apartmentBuyDocument{}
	get("apartmentBuy-energy.get.xml", &apartmentBuyKind.realEstateType, energy)
	a, err := energy.toModel("325478305", &apartmentBuyModel{})
	if err != nil {
		t.Fatal(err)
	}
	cert := a.EnergyCertificate
	if cert == nil || cert.Availability.ValueString() != "AVAILABLE" || cert.CreationDate.ValueString() != "FROM_01_MAY_2014" ||
		cert.EfficiencyClass.ValueString() != "B" || a.ConstructionYear.ValueInt64() != 1990 ||
		a.HeatingType.ValueString() != "CENTRAL_HEATING" || !slices.Equal(setStrings(a.EnergySources), []string{"GAS"}) ||
		a.BuildingEnergyRatingType.ValueString() != "ENERGY_CONSUMPTION" || a.ThermalCharacteristic.ValueFloat64() != 95.5 ||
		a.EnergyConsumptionContainsWarmWater.ValueString() != "NOT_APPLICABLE" || a.PurchasePrice.ValueFloat64() != 99000 ||
		a.LivingSpace.ValueFloat64() != 50 || a.NumberOfRooms.ValueFloat64() != 2 || a.Rented.ValueString() != "NOT_APPLICABLE" ||
		a.ContactID.ValueString() != fakeDefaultContactID || a.Address.Coordinates != nil {
		t.Fatalf("apartmentBuy with an energy certificate: %+v, certificate %+v", a, cert)
	}

	house := &houseRentDocument{}
	get("houseRent.get.xml", &houseRentKind.realEstateType, house)
	h, err := house.toModel("325478306", &houseRentModel{})
	if err != nil {
		t.Fatal(err)
	}
	if h.BuildingType.ValueString() != "MULTI_FAMILY_HOUSE" || h.PlotArea.ValueFloat64() != 125.72 ||
		h.NumberOfRooms.ValueFloat64() != 22 || h.BaseRent.ValueFloat64() != 986.17 || h.EnergyCertificate != nil ||
		!slices.Equal(setStrings(h.EnergySources), []string{"NO_INFORMATION"}) || h.PetsAllowed.ValueString() != "NO_INFORMATION" {
		t.Fatalf("houseRent: %+v", h)
	}

	sale := &houseBuyDocument{}
	get("houseBuy.get.xml", &houseBuyKind.realEstateType, sale)
	b, err := sale.toModel("325478307", &houseBuyModel{})
	if err != nil {
		t.Fatal(err)
	}
	if b.BuildingType.ValueString() != "CASTLE_MANOR_HOUSE" || b.PurchasePrice.ValueFloat64() != 750000 ||
		b.PlotArea.ValueFloat64() != 955000 || b.NumberOfRooms.ValueFloat64() != 8 || b.Rented.ValueString() != "NOT_APPLICABLE" {
		t.Fatalf("houseBuy: %+v", b)
	}
}

// A listing of another type fails with an error that names both types.
func TestListingsRejectOtherTypes(t *testing.T) {
	bodies := map[string][]byte{
		"apartmentBuy": sandboxFile(t, "apartmentBuy.get.xml"),
		"houseRent":    sandboxFile(t, "houseRent.get.xml"),
		"houseBuy":     sandboxFile(t, "houseBuy.get.xml"),
	}
	for _, typ := range []*realEstateType{&apartmentRentKind.realEstateType, &apartmentBuyKind.realEstateType,
		&houseRentKind.realEstateType, &houseBuyKind.realEstateType} {
		for other, body := range bodies {
			if other == typ.root {
				continue
			}
			err := unmarshalListing(typ, body, &struct{}{})
			if err == nil || !strings.Contains(err.Error(), "realestates:"+typ.root) || !strings.Contains(err.Error(), "<"+other+">") ||
				!strings.Contains(err.Error(), typ.plural) {
				t.Errorf("reading a %s as a %s: %v", other, typ.root, err)
			}
		}
	}
}

// Every real estate POST, PUT and GET asks for the newer energy sources.
func TestRealEstateCallsSendTheQueryParameter(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, documentedCreatedBody)
		case http.MethodGet:
			_, _ = io.WriteString(w, `<realestates:houseBuy xmlns:realestates="`+realEstatesNamespace+`"><title>t</title></realestates:houseBuy>`)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "ck", "cs", "at", "ats", "test")
	ctx, typ := context.Background(), &houseBuyKind.realEstateType
	if _, err := c.CreateRealEstate(ctx, typ, fullHouseBuy().toDocument()); err != nil {
		t.Fatal(err)
	}
	if err := c.GetRealEstate(ctx, typ, "1", &houseBuyDocument{}); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateRealEstate(ctx, typ, "1", fullHouseBuy().toDocument()); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteRealEstate(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /offer/v1.0/user/me/realestate/?usenewenergysourceenev2014values=true",
		"GET /offer/v1.0/user/me/realestate/1?usenewenergysourceenev2014values=true",
		"PUT /offer/v1.0/user/me/realestate/1?usenewenergysourceenev2014values=true",
		"DELETE /offer/v1.0/user/me/realestate/1?",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTwoDecimals(t *testing.T) {
	// A sum at run time, unlike one of constants, is not exactly 0.3.
	tenth, fifth := 0.1, 0.2
	for value, ok := range map[float64]bool{
		0: true, 50.5: true, 50.55: true, 9999999999999.99: true, 99000: true,
		50.555: false, 0.001: false, 1e-7: false, tenth + fifth: false,
	} {
		resp := &validator.Float64Response{}
		twoDecimals.ValidateFloat64(context.Background(), validator.Float64Request{
			Path: path.Root("living_space"), ConfigValue: types.Float64Value(value),
		}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%v: errors %v, want ok=%v", value, resp.Diagnostics, ok)
		}
	}
	for _, v := range []types.Float64{types.Float64Null(), types.Float64Unknown()} {
		resp := &validator.Float64Response{}
		twoDecimals.ValidateFloat64(context.Background(), validator.Float64Request{ConfigValue: v}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%v: %v", v, resp.Diagnostics)
		}
	}
}

func TestEnergySources(t *testing.T) {
	set := func(values ...string) types.Set {
		elements := make([]attr.Value, len(values))
		for i, v := range values {
			elements[i] = types.StringValue(v)
		}
		return types.SetValueMust(types.StringType, elements)
	}
	for _, tc := range []struct {
		value types.Set
		ok    bool
	}{
		{set("NO_INFORMATION"), true}, {set("GAS", "OIL"), true}, {types.SetUnknown(types.StringType), true},
		{set("NO_INFORMATION", "GAS"), false},
	} {
		resp := &validator.SetResponse{}
		noEnergySourceAlone{}.ValidateSet(context.Background(), validator.SetRequest{ConfigValue: tc.value}, resp)
		if resp.Diagnostics.HasError() == tc.ok {
			t.Errorf("%v: errors %v, want ok=%v", tc.value, resp.Diagnostics, tc.ok)
		}
	}

	prior := set("OIL", "GAS", "SOLAR_HEATING")
	if got := readEnergySources(&energySourcesElement{Sources: []string{"SOLAR_HEATING", "OIL", "GAS"}}, prior); !got.Equal(prior) {
		t.Errorf("the same sources in another order: %v", got)
	}
	if got := readEnergySources(&energySourcesElement{Sources: []string{"GAS", "GAS"}}, prior); !got.Equal(set("GAS")) {
		t.Errorf("changed sources: %v", got)
	}
	if got := readEnergySources(nil, prior); !got.IsNull() {
		t.Errorf("no energy sources in the response: %v", got)
	}
	doc := fullModel().toBuildingFields()
	if !slices.Equal(doc.EnergySources.Sources, []string{"GAS"}) {
		t.Errorf("sent sources: %v", doc.EnergySources)
	}
	m := &listingModel{}
	readBuilding(&parser{}, &buildingFields{EnergyCertificate: &energyCertificateElement{CreationDate: "FROM_01_MAY_2014"}}, &listingModel{}, m)
	if m.EnergyCertificate != nil {
		t.Errorf("a certificate without availability reads as %+v", m.EnergyCertificate)
	}
}

// The enumerations of the schema are those of the live XSD.
func TestEnumerationsMatchXSD(t *testing.T) {
	x := liveXSD(t)
	for name, tc := range map[string]struct {
		values  []string
		xsdType string
	}{
		"apartment_type":                  {apartmentTypes, "ApartmentType"},
		"building_type":                   {buildingTypes, "BuildingType"},
		"cellar":                          {yesNotApplicable, "YesNotApplicableType"},
		"heating_costs_in_service_charge": {yesNoNotApplicable, "YesNoNotApplicableType"},
		"pets_allowed":                    {petsAllowedValues, "PetsAllowedType"},
		"availability":                    {energyCertificateAvailabilities, "EnergyCertificateAvailability"},
		"creation_date":                   {energyCertificateCreationDates, "EnergyCertificateCreationDate"},
		"building_energy_rating_type":     {buildingEnergyRatingTypes, "BuildingEnergyRatingType"},
		"heating_type":                    {append([]string{"NO_INFORMATION"}, heatingTypes...), "HeatingTypeEnev2014"},
		"energy_sources":                  {energySources, "EnergySourceEnev2014"},
	} {
		want := x.enumeration(xml.Name{Space: commonNamespace, Local: tc.xsdType})
		if !slices.Equal(slices.Sorted(slices.Values(tc.values)), slices.Sorted(slices.Values(want))) {
			t.Errorf("%s: schema %v, XSD %s %v", name, tc.values, tc.xsdType, want)
		}
	}
}

// An attribute defined twice, in the shared ones or in another map of the
// type, is a bug in the provider and panics; the four schemas have none.
func TestListingSchemaRejectsDuplicates(t *testing.T) {
	for _, s := range []func() schema.Schema{apartmentRentSchema, apartmentBuySchema, houseRentSchema, houseBuySchema} {
		s()
	}
	extra := schema.StringAttribute{Optional: true}
	for name, own := range map[string][]map[string]schema.Attribute{
		"title":     {{"title": extra}},
		"base_rent": {rentAttributes(), {"base_rent": extra}},
	} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), `attribute "`+name+`" is defined twice`) {
					t.Errorf("a second %s: recovered %v, want a panic", name, r)
				}
			}()
			listingSchema("A test listing", "listing", own...)
		}()
	}
}

// A number from the API must be a finite number: strconv.ParseFloat also takes
// NaN and the infinities, and types.Float64Value panics on a NaN. An integer
// must fit an int64.
func TestParserRejectsWhatIsNoNumber(t *testing.T) {
	for _, raw := range []string{"NaN", "nan", "Inf", "+Inf", "-inf", "infinity", "-Infinity", "1e400", "abc"} {
		p := &parser{}
		if got := p.float64("baseRent", &raw, types.Float64Null()); !got.IsNull() || p.err == nil ||
			!strings.Contains(p.err.Error(), "which is not a number") {
			t.Errorf("float64(%q) = %v, error %v", raw, got, p.err)
		}
		p = &parser{}
		if got := p.int64("floor", &raw); !got.IsNull() || p.err == nil || !strings.Contains(p.err.Error(), "which is not a number") {
			t.Errorf("int64(%q) = %v, error %v", raw, got, p.err)
		}
	}
	for raw, want := range map[string]string{
		"4": "", "4.0": "", "-3": "", "-9223372036854775808": "",
		"4.5": "which is not a whole number", "1e19": "out of the range", "-1e19": "out of the range",
		"9.3e18": "out of the range",
	} {
		p := &parser{}
		got := p.int64("floor", &raw)
		if want == "" && (p.err != nil || got.IsNull()) || want != "" && (p.err == nil || !strings.Contains(p.err.Error(), want) || !got.IsNull()) {
			t.Errorf("int64(%q) = %v, error %v, want %q", raw, got, p.err, want)
		}
	}
	doc := fullModel().toDocument()
	nan := "NaN"
	doc.BaseRent = &nan
	if _, err := doc.toModel("1", fullModel()); err == nil || !strings.Contains(err.Error(), `"NaN" for baseRent`) {
		t.Errorf("a listing with the baseRent NaN: %v", err)
	}
}
