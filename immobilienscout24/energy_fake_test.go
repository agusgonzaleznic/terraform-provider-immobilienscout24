package immobilienscout24

// Tests of the energy fields of listings on the fake API: every sandbox fact
// in fake_api_energy_test.go has a test here that fails when the fake stops
// reproducing it. The helpers are in listing_fake_test.go.

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The rules of the energy fields, each a 412 ERROR_RESOURCE_VALIDATION whose
// text names the code, and combinations the sandbox accepted. The plan-time
// rules of the provider keep most of these requests from the fake, so only
// this test sees them.
func TestFakeEnforcesEnergyRules(t *testing.T) {
	s := func(v string) *string { return &v }
	cert := func(availability, date, class string) *energyCertificateElement {
		return &energyCertificateElement{Availability: availability, CreationDate: date, EfficiencyClass: class}
	}
	gas := &energySourcesElement{Sources: []string{"GAS"}}
	const (
		class     = "EV_EFFICIENCY_CLASS_NOT_VALID_FOR_THIS_CERTIFICATE"
		noThermal = "ENERGY_CONSUMPTION_CONTAINS_WARM_WATER_WITHOUT_ENERGY_CONSUMPTION"
		warm      = "EV_WARM_WATER_FIELD_NOT_VALID_FOR_THIS_CERTIFICATE"
		filled    = "EV_NOT_AVAILABLE_BUT_FIELDS_FILLED"
	)
	for _, root := range fakeListingRoots {
		t.Run(root, func(t *testing.T) {
			l := newFakeListings(t)
			for _, tc := range []struct {
				name string
				b    buildingFields
				code string // empty when the sandbox accepted it
			}{
				{"class without a creation date", buildingFields{EnergyCertificate: cert("AVAILABLE", "", "B"), BuildingEnergyRatingType: "ENERGY_CONSUMPTION"}, class},
				{"class before May 2014", buildingFields{EnergyCertificate: cert("AVAILABLE", "BEFORE_01_MAY_2014", "B"), BuildingEnergyRatingType: "ENERGY_CONSUMPTION"}, class},
				{"class without a rating type", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "B")}, class},
				{"class with the rating type NO_INFORMATION", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "B"), BuildingEnergyRatingType: "NO_INFORMATION"}, class},
				{"class NOT_APPLICABLE without a rating type", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "NOT_APPLICABLE")}, class},
				{"class B", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "B"), BuildingEnergyRatingType: "ENERGY_REQUIRED"}, ""},
				{"class A+", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "A+"), BuildingEnergyRatingType: "ENERGY_REQUIRED", ThermalCharacteristic: s("25")}, ""},
				{"warm water without a thermal characteristic", buildingFields{EnergyConsumptionContainsWarmWater: "YES"}, noThermal},
				{"warm water from May 2014", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", ""), BuildingEnergyRatingType: "ENERGY_CONSUMPTION", ThermalCharacteristic: s("95.5"), EnergyConsumptionContainsWarmWater: "YES"}, warm},
				{"warm water with an energy requirement", buildingFields{EnergyCertificate: cert("AVAILABLE", "BEFORE_01_MAY_2014", ""), BuildingEnergyRatingType: "ENERGY_REQUIRED", ThermalCharacteristic: s("95.5"), EnergyConsumptionContainsWarmWater: "YES"}, warm},
				{"warm water before May 2014", buildingFields{EnergyCertificate: cert("AVAILABLE", "BEFORE_01_MAY_2014", ""), BuildingEnergyRatingType: "ENERGY_CONSUMPTION", ThermalCharacteristic: s("95.5"), EnergyConsumptionContainsWarmWater: "YES"}, ""},
				{"warm water without a rating type", buildingFields{EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", ""), ThermalCharacteristic: s("95.5"), EnergyConsumptionContainsWarmWater: "YES"}, ""},
				{"warm water without a certificate", buildingFields{BuildingEnergyRatingType: "ENERGY_REQUIRED", ThermalCharacteristic: s("95.5"), EnergyConsumptionContainsWarmWater: "YES"}, ""},
				{"not available yet with a creation date", buildingFields{EnergyCertificate: cert("NOT_AVAILABLE_YET", "BEFORE_01_MAY_2014", "")}, filled},
				{"not required with a thermal characteristic", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), ThermalCharacteristic: s("95.5")}, filled},
				{"not required with a rating type", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), BuildingEnergyRatingType: "ENERGY_CONSUMPTION"}, filled},
				{"not available yet with the creation date NOT_APPLICABLE", buildingFields{EnergyCertificate: cert("NOT_AVAILABLE_YET", "NOT_APPLICABLE", "")}, filled},
				{"not required with the creation date NOT_APPLICABLE", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "NOT_APPLICABLE", "")}, filled},
				{"not available yet with the rating type NO_INFORMATION", buildingFields{EnergyCertificate: cert("NOT_AVAILABLE_YET", "", ""), BuildingEnergyRatingType: "NO_INFORMATION"}, filled},
				{"not required with the rating type NO_INFORMATION", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), BuildingEnergyRatingType: "NO_INFORMATION"}, filled},
				{"not required with the rating type ENERGY_REQUIRED", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), BuildingEnergyRatingType: "ENERGY_REQUIRED"}, filled},
				{"not required with what it allows", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), ConstructionYear: s("1990"), HeatingTypeEnev2014: "CENTRAL_HEATING", EnergySources: gas}, ""},
				{"not required with the defaults sent", buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), EnergySources: &energySourcesElement{Sources: []string{"NO_INFORMATION"}}, EnergyConsumptionContainsWarmWater: "NOT_APPLICABLE"}, ""},
				{"availability alone", buildingFields{EnergyCertificate: cert("AVAILABLE", "", "")}, ""},
				{"thermal characteristic and rating type without a certificate", buildingFields{BuildingEnergyRatingType: "ENERGY_REQUIRED", ThermalCharacteristic: s("95.5")}, ""},
				{"thermal characteristic 0", buildingFields{ThermalCharacteristic: s("0")}, "NUMBER_IS_OUT_OF_RANGE"},
				{"thermal characteristic 0.01", buildingFields{ThermalCharacteristic: s("0.01")}, ""},
				{"thermal characteristic 9999.99", buildingFields{ThermalCharacteristic: s("9999.99")}, ""},
				{"construction year 999", buildingFields{ConstructionYear: s("999")}, "YEAR_INVALID"},
				{"construction year 1000", buildingFields{ConstructionYear: s("1000")}, ""},
				{"construction year 2031", buildingFields{ConstructionYear: s("2031")}, ""},
			} {
				_, err := l.write("", listingBody(t, root, minimalListingFields(), tc.b), true)
				var apiErr *APIError
				switch {
				case tc.code == "" && err != nil:
					t.Errorf("%s: refused: %v", tc.name, err)
				case tc.code != "" && (!errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusPreconditionFailed ||
					!slices.Contains(apiErr.Codes(), codeResourceValidation) || !strings.Contains(err.Error(), " : "+tc.code+"]")):
					t.Errorf("%s: %v, want a 412 with %s", tc.name, err, tc.code)
				}
			}
			_, err := l.write("", listingBody(t, root, minimalListingFields(), buildingFields{
				EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "B"), BuildingEnergyRatingType: "ENERGY_CONSUMPTION"}), true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = l.write("", listingBody(t, root, minimalListingFields(), buildingFields{
				EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "B")}), true)
			if want := "Error while validating input for the resource. [MESSAGE: " +
				"energyCertificateInformation.energyEfficiencyClass : B : " + class + "]"; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("the text of the refused class is %v, want %s", err, want)
			}
			// The texts the sandbox gave; the creation date's was seen without its prefix.
			for _, tc := range []struct {
				b    buildingFields
				want string
			}{
				{buildingFields{EnergyCertificate: cert("NOT_REQUIRED", "", ""), BuildingEnergyRatingType: "NO_INFORMATION"},
					"[MESSAGE: energyCertificateInformation.buildingEnergyRatingType : NO_INFORMATION : " + filled + "]"},
				{buildingFields{EnergyCertificate: cert("NOT_AVAILABLE_YET", "NOT_APPLICABLE", "")},
					"energyCertificateCreationDate : NO_INFORMATION : " + filled + "]"},
			} {
				if _, err := l.write("", listingBody(t, root, minimalListingFields(), tc.b), true); err == nil ||
					!strings.Contains(err.Error(), tc.want) {
					t.Errorf("the text of a refused %+v is %v, want %s", tc.b.EnergyCertificate, err, tc.want)
				}
			}
			_, err = l.write("", listingBody(t, root, minimalListingFields(), buildingFields{
				EnergyCertificate: cert("AVAILABLE", "FROM_01_MAY_2014", "A_PLUS"), BuildingEnergyRatingType: "ENERGY_REQUIRED"}), true)
			if err == nil || !strings.Contains(err.Error(), "ERROR_COMMON_SCHEMA_VALIDATION_FAILED") {
				t.Errorf("A_PLUS: %v, want a schema error", err)
			}
		})
	}
}

// The newer energy sources need the query parameter on writes, and a GET
// without it leaves the energy sources out.
func TestFakeNewerEnergySourcesNeedTheQuery(t *testing.T) {
	for _, root := range fakeListingRoots {
		t.Run(root, func(t *testing.T) {
			l := newFakeListings(t)
			for _, source := range fakeNewEnergySources {
				body := listingBody(t, root, minimalListingFields(), buildingFields{EnergySources: &energySourcesElement{Sources: []string{source}}})
				_, err := l.write("", body, false)
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusPreconditionFailed ||
					!slices.Contains(apiErr.Codes(), codeResourceValidation) ||
					!strings.Contains(err.Error(), "EnergySourceEnev2014 value ["+source+"] is not allowed.") {
					t.Fatalf("POST of %s without the query parameter: %v", source, err)
				}
				id := l.mustWrite("", body)
				if _, err := l.write(id, body, false); err == nil {
					t.Fatalf("PUT of %s without the query parameter was accepted", source)
				}
				if got := texts(l.get(id, true), "energySourcesEnev2014/energySourceEnev2014"); !slices.Equal(got, []string{source}) {
					t.Fatalf("GET with the query parameter: energy sources %v", got)
				}
				if got := l.get(id, false); texts(got, "energySourcesEnev2014/energySourceEnev2014") != nil {
					t.Fatalf("GET without the query parameter has energy sources:\n%s", strings.Join(leaves(got, ""), "\n"))
				}
			}
			old := listingBody(t, root, minimalListingFields(), buildingFields{EnergySources: &energySourcesElement{Sources: []string{"GAS"}}})
			if _, err := l.write("", old, false); err != nil {
				t.Fatalf("an older energy source without the query parameter: %v", err)
			}
		})
	}
}

// Omitted energy sources come back as NO_INFORMATION, NO_INFORMATION next to
// another source is dropped, the sources come back in an order of their own,
// and heatingTypeEnev2014 NO_INFORMATION is dropped while the other
// NO_INFORMATION and NOT_APPLICABLE values stay.
func TestFakeStoresEnergyValuesLikeTheSandbox(t *testing.T) {
	sources := func(s ...string) *energySourcesElement { return &energySourcesElement{Sources: s} }
	for _, root := range fakeListingRoots {
		t.Run(root, func(t *testing.T) {
			l := newFakeListings(t)
			for _, tc := range []struct {
				name string
				b    buildingFields
				want map[string][]string
			}{
				{"heating type without sources", buildingFields{HeatingTypeEnev2014: "CENTRAL_HEATING"},
					map[string][]string{"energySourcesEnev2014/energySourceEnev2014": {"NO_INFORMATION"}}},
				{"NO_INFORMATION next to GAS", buildingFields{EnergySources: sources("NO_INFORMATION", "GAS")},
					map[string][]string{"energySourcesEnev2014/energySourceEnev2014": {"GAS"}}},
				{"another order", buildingFields{EnergySources: sources("OIL", "GAS", "SOLAR_HEATING")},
					map[string][]string{"energySourcesEnev2014/energySourceEnev2014": {"SOLAR_HEATING", "OIL", "GAS"}}},
				{"NO_INFORMATION values", buildingFields{HeatingTypeEnev2014: "NO_INFORMATION", BuildingEnergyRatingType: "NO_INFORMATION",
					EnergyCertificate: &energyCertificateElement{Availability: "AVAILABLE", CreationDate: "NOT_APPLICABLE"}},
					map[string][]string{"heatingTypeEnev2014": nil, "buildingEnergyRatingType": {"NO_INFORMATION"},
						"energyCertificate/energyCertificateCreationDate": {"NOT_APPLICABLE"}}},
			} {
				got := l.get(l.mustWrite("", listingBody(t, root, minimalListingFields(), tc.b)), true)
				for path, want := range tc.want {
					if texts(got, path) == nil && want == nil {
						continue
					}
					if !slices.Equal(texts(got, path), want) {
						t.Errorf("%s: %s is %v, want %v", tc.name, path, texts(got, path), want)
					}
				}
			}
		})
	}
}

// The fields the sandbox derives, as observed on a houseRent (2026-09-30):
// HEAT_PUMP gives no heatingType, ELECTRICITY with ENVIRONMENTAL_THERMAL_ENERGY
// the firingType ELECTRICITY only, and a certificate AVAILABLE gets
// legalConstructionYear and the listing energyPerformanceCertificate true. The
// replay covers CENTRAL_HEATING and GAS.
func TestFakeDerivesEnergyFields(t *testing.T) {
	year, thermal := "1975", "80"
	for _, root := range fakeListingRoots {
		t.Run(root, func(t *testing.T) {
			l := newFakeListings(t)
			got := l.get(l.mustWrite("", listingBody(t, root, minimalListingFields(), buildingFields{
				EnergyCertificate: &energyCertificateElement{Availability: "AVAILABLE", CreationDate: "FROM_01_MAY_2014",
					EfficiencyClass: "C"},
				ConstructionYear: &year, HeatingTypeEnev2014: "HEAT_PUMP",
				EnergySources:            &energySourcesElement{Sources: []string{"ELECTRICITY", "ENVIRONMENTAL_THERMAL_ENERGY"}},
				BuildingEnergyRatingType: "ENERGY_REQUIRED", ThermalCharacteristic: &thermal,
			})), true)
			if texts(got, "heatingType") != nil || !slices.Equal(texts(got, "firingTypes/firingType"), []string{"ELECTRICITY"}) ||
				!slices.Equal(texts(got, "energyCertificate/legalConstructionYear"), []string{year}) ||
				!slices.Equal(texts(got, "energyPerformanceCertificate"), []string{"true"}) {
				t.Fatalf("derived fields:\n%s", strings.Join(leaves(got, ""), "\n"))
			}
		})
	}
}

// A PUT without the energy elements clears them, back to the defaults.
func TestFakePutClearsEnergy(t *testing.T) {
	year, thermal := "1990", "95.5"
	for _, root := range fakeListingRoots {
		t.Run(root, func(t *testing.T) {
			l := newFakeListings(t)
			id := l.mustWrite("", listingBody(t, root, minimalListingFields(), buildingFields{
				EnergyCertificate: &energyCertificateElement{Availability: "AVAILABLE", CreationDate: "FROM_01_MAY_2014", EfficiencyClass: "B"},
				ConstructionYear:  &year, HeatingTypeEnev2014: "CENTRAL_HEATING", EnergySources: &energySourcesElement{Sources: []string{"GAS"}},
				BuildingEnergyRatingType: "ENERGY_CONSUMPTION", ThermalCharacteristic: &thermal,
			}))
			l.mustWrite(id, listingBody(t, root, minimalListingFields(), buildingFields{}))
			got := l.get(id, true)
			for _, gone := range []string{"energyCertificate/energyCertificateAvailability", "constructionYear",
				"heatingTypeEnev2014", "heatingType", "buildingEnergyRatingType", "thermalCharacteristic", "energyPerformanceCertificate"} {
				if texts(got, gone) != nil {
					t.Errorf("%s is still there after a PUT without it", gone)
				}
			}
			if !slices.Equal(texts(got, "energySourcesEnev2014/energySourceEnev2014"), []string{"NO_INFORMATION"}) ||
				!slices.Equal(texts(got, "energyConsumptionContainsWarmWater"), []string{"NOT_APPLICABLE"}) {
				t.Errorf("the defaults are not back:\n%s", strings.Join(leaves(got, ""), "\n"))
			}
		})
	}
}
