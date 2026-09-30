package immobilienscout24

// Acceptance tests of the rules the plan checks for the listing resources:
// ValidateConfig and the two-decimal validator. A step that breaks a rule
// fails the plan, so no request reaches the fake API.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// checkNothingCreated asserts that no listing was created.
func checkNothingCreated(f *fakeAPI) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if created, _ := f.History(); len(created) > 0 {
			return fmt.Errorf("listings were created: %v", created)
		}
		return nil
	}
}

// energyRuleCases break the rules V1 to V4 of validateEnergy.
var energyRuleCases = []struct{ name, hcl, summary string }{
	{"V1 class without a creation date", `
  energy_certificate = {
    availability     = "AVAILABLE"
    efficiency_class = "B"
  }
  building_energy_rating_type = "ENERGY_CONSUMPTION"
`, "Efficiency class not valid for this certificate"},
	{"V1 class NOT_APPLICABLE with the rating type NO_INFORMATION", `
  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "NOT_APPLICABLE"
  }
  building_energy_rating_type = "NO_INFORMATION"
`, "Efficiency class not valid for this certificate"},
	{"V2 warm water without a thermal characteristic", `
  energy_consumption_contains_warm_water = "YES"
`, "Warm water without a thermal characteristic"},
	{"V3 warm water from May 2014", `
  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "FROM_01_MAY_2014"
  }
  building_energy_rating_type            = "ENERGY_CONSUMPTION"
  thermal_characteristic                 = 95.5
  energy_consumption_contains_warm_water = "YES"
`, "Warm water not valid for this certificate"},
	{"V3 warm water with an energy requirement before May 2014", `
  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "BEFORE_01_MAY_2014"
  }
  building_energy_rating_type            = "ENERGY_REQUIRED"
  thermal_characteristic                 = 95.5
  energy_consumption_contains_warm_water = "YES"
`, "Warm water not valid for this certificate"},
	{"V4 creation date", `
  energy_certificate = {
    availability  = "NOT_AVAILABLE_YET"
    creation_date = "BEFORE_01_MAY_2014"
  }
`, "Energy certificate not available but fields filled"},
	{"V4 efficiency class", `
  energy_certificate = {
    availability     = "NOT_REQUIRED"
    efficiency_class = "B"
  }
`, "Energy certificate not available but fields filled"},
	{"V4 thermal characteristic", `
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  thermal_characteristic = 95.5
`, "Energy certificate not available but fields filled"},
	{"V4 rating type ENERGY_REQUIRED, not available yet", `
  energy_certificate = {
    availability = "NOT_AVAILABLE_YET"
  }
  building_energy_rating_type = "ENERGY_REQUIRED"
`, "Energy certificate not available but fields filled"},
	{"V4 rating type ENERGY_REQUIRED, not required", `
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  building_energy_rating_type = "ENERGY_REQUIRED"
`, "Energy certificate not available but fields filled"},
	{"V4 rating type NO_INFORMATION, not available yet", `
  energy_certificate = {
    availability = "NOT_AVAILABLE_YET"
  }
  building_energy_rating_type = "NO_INFORMATION"
`, "Energy certificate not available but fields filled"},
	{"V4 rating type NO_INFORMATION, not required", `
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  building_energy_rating_type = "NO_INFORMATION"
`, "Energy certificate not available but fields filled"},
	{"V4 creation date NOT_APPLICABLE, not available yet", `
  energy_certificate = {
    availability  = "NOT_AVAILABLE_YET"
    creation_date = "NOT_APPLICABLE"
  }
`, "Energy certificate not available but fields filled"},
	{"V4 creation date NOT_APPLICABLE, not required", `
  energy_certificate = {
    availability  = "NOT_REQUIRED"
    creation_date = "NOT_APPLICABLE"
  }
`, "Energy certificate not available but fields filled"},
}

// Each rule fails the plan, so no request reaches the API.
func TestAccListingEnergy_rules(t *testing.T) {
	for _, c := range allListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			var steps []resource.TestStep
			for _, rule := range energyRuleCases {
				steps = append(steps, resource.TestStep{
					Config:      testAccProviderBlock(f.BaseURL()) + c.config("anonymized", rule.hcl),
					ExpectError: wrapped(rule.summary),
				})
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkNothingCreated(f),
				Steps:                    steps,
			})
		})
	}
}

// The commission on every listing type and the heating costs on the two types
// for rent follow the documented rules, checked at plan time.
func TestAccListing_crossFieldRules(t *testing.T) {
	for _, c := range allListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			provider := testAccProviderBlock(f.BaseURL())
			withoutCommission := strings.Replace(c.config("anonymized", ""), "    courtage     = \"7,14%\"\n", "", 1)
			steps := []resource.TestStep{{
				Config:      provider + withoutCommission,
				ExpectError: wrapped("Missing commission", `courtage.courtage is required when courtage.has_courtage is "YES"`),
			}}
			if c.name == "apartment_rent" || c.name == "house_rent" {
				for _, extra := range []string{
					"  heating_costs = 70\n",
					"  heating_costs = 70\n  heating_costs_in_service_charge = \"NOT_APPLICABLE\"\n",
				} {
					steps = append(steps, resource.TestStep{
						Config: provider + c.config("anonymized", extra),
						ExpectError: wrapped("Invalid combination",
							`Set heating_costs_in_service_charge to "YES" or "NO" when heating_costs is set.`),
					})
				}
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkNothingCreated(f),
				Steps:                    steps,
			})
		})
	}
}

// A rule is skipped while a value it reads is unknown at plan time.
func TestAccListingEnergy_rulesSkipUnknownValues(t *testing.T) {
	f := newFakeAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkNothingCreated(f),
		Steps: []resource.TestStep{{
			Config: testAccProviderBlock(f.BaseURL()) + `
resource "terraform_data" "later" {
  input = {
    warm_water  = "YES"
    certificate = { availability = "NOT_REQUIRED", creation_date = "BEFORE_01_MAY_2014", efficiency_class = "B" }
  }
}
` + apartmentRentCase.config("anonymized", `
  energy_certificate                     = terraform_data.later.output.certificate
  energy_consumption_contains_warm_water = terraform_data.later.output.warm_water
`),
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}

// Every decimal attribute but the coordinates takes at most two decimal
// places. strings.Replace puts a third into a required attribute.
func TestAccListing_twoDecimals(t *testing.T) {
	f := newFakeAPI(t)
	apartmentBuy, houseRent, houseBuy := newListingCases[0], newListingCases[1], newListingCases[2]
	var steps []resource.TestStep
	for _, config := range []string{
		apartmentRentCase.config("anonymized", "  thermal_characteristic = 95.555\n"),
		apartmentRentCase.config("anonymized", "  heating_costs = 70.125\n  heating_costs_in_service_charge = \"NO\"\n"),
		strings.Replace(apartmentRentCase.config("anonymized", ""), "living_space    = 72", "living_space    = 50.555", 1),
		apartmentBuy.config("anonymized", "  service_charge = 250.555\n"),
		strings.Replace(apartmentBuy.config("anonymized", ""), "purchase_price  = 99000", "purchase_price  = 99000.001", 1),
		strings.Replace(houseRent.config("anonymized", ""), "plot_area       = 125.72", "plot_area       = 125.717", 1),
		strings.Replace(houseRent.config("anonymized", ""), "base_rent       = 986.17", "base_rent       = 986.175", 1),
		strings.Replace(houseBuy.config("anonymized", ""), "number_of_rooms = 6", "number_of_rooms = 6.125", 1),
	} {
		steps = append(steps, resource.TestStep{
			Config:      testAccProviderBlock(f.BaseURL()) + config,
			ExpectError: wrapped("Too many decimal places", "more than two decimal places"),
		})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkNothingCreated(f),
		Steps:                    steps,
	})
}
