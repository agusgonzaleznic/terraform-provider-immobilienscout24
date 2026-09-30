package immobilienscout24

// Acceptance tests of the energy attributes and the two-decimal validator
// against the fake API, for all four listing types.

import (
	"fmt"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// allListingCases are the four listing types.
var allListingCases = append([]listingCase{apartmentRentCase}, newListingCases...)

// checkFakeTexts asserts the texts at a path of a listing on the fake API,
// such as "energyCertificate/energyCertificateAvailability"; no want asserts
// that there are none.
func checkFakeTexts(f *fakeAPI, id *string, path string, want ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		obj, ok := f.Object(*id)
		if !ok {
			return fmt.Errorf("object %s does not exist on the fake API", *id)
		}
		if got := texts(obj, path); !slices.Equal(got, want) {
			return fmt.Errorf("object %s has %s %v, want %v", *id, path, got, want)
		}
		return nil
	}
}

// A full certificate AVAILABLE, then NOT_REQUIRED, then no energy attributes
// at all, which is back to the API's defaults. Each apply leaves no
// difference, although the API derives fields of its own.
func TestAccListingEnergy_certificate(t *testing.T) {
	available := `
  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "B"
  }
  construction_year           = 1990
  heating_type                = "CENTRAL_HEATING"
  energy_sources              = ["GAS"]
  building_energy_rating_type = "ENERGY_CONSUMPTION"
  thermal_characteristic      = 95.5
`
	notRequired := `
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  construction_year = 1990
  heating_type      = "CENTRAL_HEATING"
  energy_sources    = ["GAS"]
`
	for _, c := range allListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			var id string
			name, provider := c.resourceName(), testAccProviderBlock(f.BaseURL())
			update := resource.ConfigPlanChecks{
				PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkDestroyed(f),
				Steps: []resource.TestStep{
					{
						Config: provider + c.config("anonymized", available),
						Check: resource.ComposeAggregateTestCheckFunc(
							captureResourceID(name, &id),
							resource.TestCheckResourceAttr(name, "energy_certificate.availability", "AVAILABLE"),
							resource.TestCheckResourceAttr(name, "energy_certificate.efficiency_class", "B"),
							resource.TestCheckResourceAttr(name, "construction_year", "1990"),
							resource.TestCheckTypeSetElemAttr(name, "energy_sources.*", "GAS"),
							resource.TestCheckResourceAttr(name, "thermal_characteristic", "95.5"),
							checkFakeTexts(f, &id, "energyCertificate/energyCertificateCreationDate", "FROM_01_MAY_2014"),
							checkFakeTexts(f, &id, "buildingEnergyRatingType", "ENERGY_CONSUMPTION"),
							// Derived by the API, and not managed.
							checkFakeTexts(f, &id, "energyCertificate/legalConstructionYear", "1990"),
							checkFakeTexts(f, &id, "heatingType", "CENTRAL_HEATING"),
							checkFakeTexts(f, &id, "firingTypes/firingType", "GAS"),
						),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
					{
						ResourceName:      name,
						ImportState:       true,
						ImportStateVerify: true,
					},
					{
						Config:           provider + c.config("anonymized", notRequired),
						ConfigPlanChecks: update,
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(name, "energy_certificate.availability", "NOT_REQUIRED"),
							resource.TestCheckNoResourceAttr(name, "energy_certificate.creation_date"),
							resource.TestCheckNoResourceAttr(name, "thermal_characteristic"),
							checkFakeTexts(f, &id, "energyCertificate/energyCertificateAvailability", "NOT_REQUIRED"),
							checkFakeTexts(f, &id, "energyCertificate/energyEfficiencyClass"),
							checkFakeTexts(f, &id, "thermalCharacteristic"),
							checkFakeTexts(f, &id, "buildingEnergyRatingType"),
						),
					},
					{
						Config:           provider + c.config("anonymized", ""),
						ConfigPlanChecks: update,
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckNoResourceAttr(name, "energy_certificate"),
							resource.TestCheckNoResourceAttr(name, "construction_year"),
							resource.TestCheckNoResourceAttr(name, "heating_type"),
							resource.TestCheckResourceAttr(name, "energy_sources.#", "1"),
							resource.TestCheckTypeSetElemAttr(name, "energy_sources.*", "NO_INFORMATION"),
							resource.TestCheckResourceAttr(name, "energy_consumption_contains_warm_water", "NOT_APPLICABLE"),
							checkFakeTexts(f, &id, "energyCertificate/energyCertificateAvailability"),
							checkFakeTexts(f, &id, "constructionYear"),
							checkFakeTexts(f, &id, "energySourcesEnev2014/energySourceEnev2014", "NO_INFORMATION"),
						),
					},
				},
			})
		})
	}
}

// Removing energy_sources from the configuration, and nothing else, returns
// the listing to the API's default.
func TestAccListingEnergy_sourcesBackToDefault(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	c := newListingCases[2]
	name, provider := c.resourceName(), testAccProviderBlock(f.BaseURL())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: provider + c.config("anonymized", "  energy_sources = [\"GAS\"]\n"),
				Check:  captureResourceID(name, &id),
			},
			{
				Config: provider + c.config("anonymized", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemAttr(name, "energy_sources.*", "NO_INFORMATION"),
					checkFakeTexts(f, &id, "energySourcesEnev2014/energySourceEnev2014", "NO_INFORMATION"),
				),
			},
		},
	})
}

// The API returns the energy sources in an order of its own; as a set, they
// leave no difference.
func TestAccListingEnergy_sourcesInAnotherOrder(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	c := apartmentRentCase
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: testAccProviderBlock(f.BaseURL()) + c.config("anonymized", `
  energy_sources = ["OIL", "GAS", "SOLAR_HEATING"]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(c.resourceName(), &id),
					checkLastRealEstateWrite(f, "POST", func() string {
						return "<energySourcesEnev2014><energySourceEnev2014>GAS</energySourceEnev2014>" +
							"<energySourceEnev2014>OIL</energySourceEnev2014><energySourceEnev2014>SOLAR_HEATING</energySourceEnev2014>"
					}, true),
					checkFakeTexts(f, &id, "energySourcesEnev2014/energySourceEnev2014", "SOLAR_HEATING", "GAS", "OIL"),
					resource.TestCheckResourceAttr(c.resourceName(), "energy_sources.#", "3"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      c.resourceName(),
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The newer energy sources work on create, read and update, which shows that
// every POST, GET and PUT sends the query parameter the fake insists on.
func TestAccListingEnergy_newerEnergySources(t *testing.T) {
	for _, c := range allListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			var id string
			name, provider := c.resourceName(), testAccProviderBlock(f.BaseURL())
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkDestroyed(f),
				Steps: []resource.TestStep{
					{
						Config: provider + c.config("anonymized", `
  heating_type   = "HEAT_PUMP"
  energy_sources = ["ENVIRONMENTAL_THERMAL_ENERGY", "ELECTRICITY"]
`),
						Check: resource.ComposeAggregateTestCheckFunc(
							captureResourceID(name, &id),
							resource.TestCheckTypeSetElemAttr(name, "energy_sources.*", "ENVIRONMENTAL_THERMAL_ENERGY"),
							checkFakeTexts(f, &id, "heatingType"),
						),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
					{
						Config: provider + c.config("anonymized", `
  heating_type   = "HEAT_PUMP"
  energy_sources = ["WIND_ENERGY", "COMBINED_HEAT_AND_POWER_BIO_ENERGY"]
`),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckTypeSetElemAttr(name, "energy_sources.*", "WIND_ENERGY"),
							checkFakeTexts(f, &id, "energySourcesEnev2014/energySourceEnev2014",
								"WIND_ENERGY", "COMBINED_HEAT_AND_POWER_BIO_ENERGY"),
						),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
				},
			})
		})
	}
}

// Combinations the sandbox accepted pass the rules and apply.
func TestAccListingEnergy_acceptedCombinations(t *testing.T) {
	f := newFakeAPI(t)
	c := newListingCases[1]
	var steps []resource.TestStep
	for _, hcl := range []string{
		// Availability AVAILABLE alone.
		`
  energy_certificate = {
    availability = "AVAILABLE"
  }
`,
		// AVAILABLE without a creation date, and from May 2014 without a class.
		`
  energy_certificate = {
    availability = "AVAILABLE"
  }
  building_energy_rating_type = "ENERGY_CONSUMPTION"
  thermal_characteristic      = 95.5
`, `
  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "FROM_01_MAY_2014"
  }
  building_energy_rating_type = "ENERGY_REQUIRED"
  thermal_characteristic      = 25
`,
		// Warm water with a consumption certificate from before May 2014, and
		// with either the creation date or the rating type left out.
		`
  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "BEFORE_01_MAY_2014"
  }
  building_energy_rating_type            = "ENERGY_CONSUMPTION"
  thermal_characteristic                 = 95.5
  energy_consumption_contains_warm_water = "YES"
`, `
  building_energy_rating_type            = "ENERGY_REQUIRED"
  thermal_characteristic                 = 95.5
  energy_consumption_contains_warm_water = "YES"
`, `
  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "FROM_01_MAY_2014"
  }
  thermal_characteristic                 = 95.5
  energy_consumption_contains_warm_water = "YES"
`,
		// A class A+, the lowest and highest thermal characteristic.
		`
  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "A+"
  }
  building_energy_rating_type = "ENERGY_REQUIRED"
  thermal_characteristic      = 0.01
`, `
  building_energy_rating_type = "ENERGY_CONSUMPTION"
  thermal_characteristic      = 9999.99
`,
		// NOT_REQUIRED with what it allows, the defaults of energy_sources
		// and hot water sent explicitly too.
		`
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  construction_year = 1000
  heating_type      = "GAS_HEATING"
  energy_sources    = ["GAS"]
`, `
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  energy_sources                         = ["NO_INFORMATION"]
  energy_consumption_contains_warm_water = "NOT_APPLICABLE"
`,
	} {
		steps = append(steps, resource.TestStep{
			Config: testAccProviderBlock(f.BaseURL()) + c.config("anonymized", hcl),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
		})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps:                    steps,
	})
}
