package immobilienscout24

// The energy fields of a listing on the fake API in fake_api_test.go. They
// mirror what the live sandbox did on 2026-09-30, the same for all four
// listing types:
//
//   - the eight newer energy sources in fakeNewEnergySources need the query
//     parameter usenewenergysourceenev2014values=true. A POST or PUT without it
//     that sends one answers 412, and a GET without it leaves
//     energySourcesEnev2014 out altogether;
//   - omitted energy sources come back as one NO_INFORMATION entry, also with a
//     heating type set, and NO_INFORMATION next to another source is dropped.
//     The sources come back in an order of their own: OIL, GAS, SOLAR_HEATING
//     came back SOLAR_HEATING, OIL, GAS, and the fake always moves the last one
//     to the front;
//   - heatingTypeEnev2014 NO_INFORMATION is dropped, while
//     buildingEnergyRatingType NO_INFORMATION and energyCertificateCreationDate
//     NOT_APPLICABLE are kept;
//   - with availability AVAILABLE, the certificate gets legalConstructionYear
//     from constructionYear and the listing energyPerformanceCertificate true;
//   - heatingType is derived from heatingTypeEnev2014 and firingTypes from the
//     energy sources: CENTRAL_HEATING gave CENTRAL_HEATING and HEAT_PUMP none,
//     GAS gave GAS, and ELECTRICITY with ENVIRONMENTAL_THERMAL_ENERGY gave
//     ELECTRICITY only;
//   - the rules in checkEnergy, each a 412 ERROR_RESOURCE_VALIDATION whose
//     text names the field, the value and the code. With availability
//     NOT_AVAILABLE_YET or NOT_REQUIRED, any creation date and any rating type
//     is refused, NOT_APPLICABLE and NO_INFORMATION included, while energy
//     sources NO_INFORMATION and hot water NOT_APPLICABLE are taken.
//
// Simulated, not observed: that the derived fields take the value of the same
// name in the older type for the other values, NO_INFORMATION when none has
// one, and that they are derived without a certificate too; the field names in
// the texts of the 412s other than energyEfficiencyClass and
// buildingEnergyRatingType (energyCertificateCreationDate was seen without its
// prefix), the value a refused creation date other than NOT_APPLICABLE shows,
// and which field is named when several are filled; how an efficiency class
// outside the observed ones fails the schema.

import (
	"fmt"
	"slices"
	"strconv"
)

// fakeNewEnergySourcesParam is the query parameter for the newer energy sources.
const fakeNewEnergySourcesParam = "usenewenergysourceenev2014values"

// fakeNewEnergySources are the energy sources that need fakeNewEnergySourcesParam.
var fakeNewEnergySources = []string{"BIO_ENERGY", "WIND_ENERGY", "HYDRO_ENERGY", "ENVIRONMENTAL_THERMAL_ENERGY",
	"COMBINED_HEAT_AND_POWER_FOSSIL_FUELS", "COMBINED_HEAT_AND_POWER_RENEWABLE_ENERGY",
	"COMBINED_HEAT_AND_POWER_REGENERATIVE_ENERGY", "COMBINED_HEAT_AND_POWER_BIO_ENERGY"}

// fakeEfficiencyClasses are the efficiency classes the sandbox takes; the
// XSD declares an xs:string.
var fakeEfficiencyClasses = []string{"NOT_APPLICABLE", "A+", "A", "B", "C", "D", "E", "F", "G", "H"}

// fakeOlderHeatingTypes and fakeFiringTypes are the values of the older
// heatingType and firingType that the fake derives, those of the same name in
// the newer types.
var (
	fakeOlderHeatingTypes = []string{"SELF_CONTAINED_CENTRAL_HEATING", "STOVE_HEATING", "CENTRAL_HEATING"}
	fakeFiringTypes       = []string{"GEOTHERMAL", "SOLAR_HEATING", "PELLET_HEATING", "GAS", "OIL", "DISTRICT_HEATING",
		"ELECTRICITY", "COAL"}
)

// errValidation is the sandbox's 412 ERROR_RESOURCE_VALIDATION for a listing
// that breaks a rule of its fields.
type errValidation struct{ text string }

func (e errValidation) Error() string { return e.text }

// fieldInvalid is an errValidation in the shape of the sandbox's texts, such
// as "... [MESSAGE: energyCertificateInformation.energyEfficiencyClass : B :
// EV_EFFICIENCY_CLASS_NOT_VALID_FOR_THIS_CERTIFICATE]".
func fieldInvalid(field, value, code string) errValidation {
	return errValidation{"Error while validating input for the resource. [MESSAGE: " + field + " : " + value + " : " + code + "]"}
}

// checkNewEnergySources refuses the newer energy sources in a write that
// comes without the query parameter, with the sandbox's text.
func checkNewEnergySources(obj *xnode) error {
	if s := obj.child("energySourcesEnev2014"); s != nil {
		for _, c := range s.Children {
			if slices.Contains(fakeNewEnergySources, c.Text) {
				return errValidation{"ERROR_RESOURCE_VALIDATION, parameters: [EnergySourceEnev2014 value [" + c.Text + "] is not allowed.]"}
			}
		}
	}
	return nil
}

// checkEnergy checks the energy elements of a listing request: their form
// against the XSD fixture, then the sandbox's rules.
func (f *fakeAPI) checkEnergy(root *xnode) error {
	cert := root.child("energyCertificate")
	if cert != nil {
		seen := map[string]bool{}
		for _, c := range cert.Children {
			e := elementNamed(f.listing.certificate, c.Name)
			switch {
			case e.Name == "":
				return fmt.Errorf("energyCertificate: element <%s> is not in common:EnergyPerformanceCertificate", c.Name)
			case seen[c.Name]:
				return fmt.Errorf("energyCertificate: element <%s> appears twice in an xs:all", c.Name)
			}
			seen[c.Name] = true
			if err := f.listing.checkValue("energyCertificate", c, e); err != nil {
				return err
			}
		}
	}
	if s := root.child("energySourcesEnev2014"); s != nil {
		if err := checkOrder("energySourcesEnev2014", childNames(s), f.listing.energySources); err != nil {
			return err
		}
		for _, c := range s.Children {
			if err := f.listing.checkValue("energySourcesEnev2014", c, f.listing.energySources[0]); err != nil {
				return err
			}
		}
	}

	text := func(parent *xnode, name string) string {
		if c := parent.child(name); c != nil {
			return c.Text
		}
		return ""
	}
	var availability, date, class string
	if cert != nil {
		availability = text(cert, "energyCertificateAvailability")
		date = text(cert, "energyCertificateCreationDate")
		class = text(cert, "energyEfficiencyClass")
	}
	rating, thermal := text(root, "buildingEnergyRatingType"), text(root, "thermalCharacteristic")
	warmWater, year := text(root, "energyConsumptionContainsWarmWater"), text(root, "constructionYear")
	rated := rating == "ENERGY_REQUIRED" || rating == "ENERGY_CONSUMPTION"
	dated := date == "BEFORE_01_MAY_2014" || date == "FROM_01_MAY_2014"

	if class != "" && !slices.Contains(fakeEfficiencyClasses, class) {
		return fmt.Errorf("energyCertificate: <energyEfficiencyClass>%s</energyEfficiencyClass> is not an efficiency class", class)
	}
	if v, err := strconv.ParseFloat(thermal, 64); thermal != "" && (err != nil || v <= 0) {
		return fieldInvalid("thermalCharacteristic", thermal, "NUMBER_IS_OUT_OF_RANGE")
	}
	if y, err := strconv.Atoi(year); year != "" && (err != nil || y < 1000) {
		return fieldInvalid("constructionYear", year, "YEAR_INVALID")
	}
	if availability == "NOT_AVAILABLE_YET" || availability == "NOT_REQUIRED" {
		const filled = "EV_NOT_AVAILABLE_BUT_FIELDS_FILLED"
		switch {
		case date != "":
			// The sandbox named a creation date NOT_APPLICABLE as NO_INFORMATION.
			if date == "NOT_APPLICABLE" {
				date = "NO_INFORMATION"
			}
			return fieldInvalid("energyCertificateInformation.energyCertificateCreationDate", date, filled)
		case rating != "":
			return fieldInvalid("energyCertificateInformation.buildingEnergyRatingType", rating, filled)
		case thermal != "":
			return fieldInvalid("energyCertificateInformation.thermalCharacteristic", thermal, filled)
		}
	}
	if class != "" && (date != "FROM_01_MAY_2014" || !rated) {
		return fieldInvalid("energyCertificateInformation.energyEfficiencyClass", class,
			"EV_EFFICIENCY_CLASS_NOT_VALID_FOR_THIS_CERTIFICATE")
	}
	if warmWater == "YES" && thermal == "" {
		return fieldInvalid("energyCertificateInformation.energyConsumptionContainsWarmWater", warmWater,
			"ENERGY_CONSUMPTION_CONTAINS_WARM_WATER_WITHOUT_ENERGY_CONSUMPTION")
	}
	if warmWater == "YES" && dated && rated && (date != "BEFORE_01_MAY_2014" || rating != "ENERGY_CONSUMPTION") {
		return fieldInvalid("energyCertificateInformation.energyConsumptionContainsWarmWater", warmWater,
			"EV_WARM_WATER_FIELD_NOT_VALID_FOR_THIS_CERTIFICATE")
	}
	return nil
}

// completeEnergy fills in and derives the energy fields of a listing as the
// sandbox does; completeListing then sorts them into place.
func completeEnergy(obj *xnode) {
	if h := obj.child("heatingTypeEnev2014"); h != nil && h.Text == "NO_INFORMATION" {
		obj.Children = slices.DeleteFunc(obj.Children, func(c *xnode) bool { return c == h })
	}
	sources := obj.child("energySourcesEnev2014")
	if sources == nil {
		sources = &xnode{Name: "energySourcesEnev2014"}
		obj.Children = append(obj.Children, sources)
	}
	if len(sources.Children) > 1 {
		sources.Children = slices.DeleteFunc(sources.Children, func(c *xnode) bool { return c.Text == "NO_INFORMATION" })
	}
	if n := len(sources.Children); n > 1 {
		sources.Children = append([]*xnode{sources.Children[n-1]}, sources.Children[:n-1]...)
	}
	if len(sources.Children) == 0 {
		sources.Children = []*xnode{{Name: "energySourceEnev2014", Text: "NO_INFORMATION"}}
	}

	if h := obj.child("heatingTypeEnev2014"); h != nil && obj.child("heatingType") == nil &&
		slices.Contains(fakeOlderHeatingTypes, h.Text) {
		obj.Children = append(obj.Children, &xnode{Name: "heatingType", Text: h.Text})
	}
	if obj.child("firingTypes") == nil {
		firing := &xnode{Name: "firingTypes"}
		for _, s := range sources.Children {
			if slices.Contains(fakeFiringTypes, s.Text) {
				firing.Children = append(firing.Children, &xnode{Name: "firingType", Text: s.Text})
			}
		}
		if len(firing.Children) == 0 {
			firing.Children = []*xnode{{Name: "firingType", Text: "NO_INFORMATION"}}
		}
		obj.Children = append(obj.Children, firing)
	}
	cert := obj.child("energyCertificate")
	if cert == nil || cert.child("energyCertificateAvailability") == nil ||
		cert.child("energyCertificateAvailability").Text != "AVAILABLE" {
		return
	}
	if y := obj.child("constructionYear"); y != nil && cert.child("legalConstructionYear") == nil {
		cert.Children = append(cert.Children, &xnode{Name: "legalConstructionYear", Text: y.Text})
	}
	if obj.child("energyPerformanceCertificate") == nil {
		obj.Children = append(obj.Children, &xnode{Name: "energyPerformanceCertificate", Text: "true"})
	}
}
