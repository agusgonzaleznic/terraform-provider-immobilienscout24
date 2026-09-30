package immobilienscout24

// Live acceptance tests of the listing types besides apartment rent, and of
// the energy attributes on apartment rent, against the real ImmobilienScout24
// sandbox. Like TestAccApartmentRent_liveSandbox, they run only with TF_ACC=1,
// IMMOBILIENSCOUT24_LIVE=1 and all four IMMOBILIENSCOUT24_* credential
// variables set to sandbox credentials, and skip otherwise. Each creates a
// listing, plans again, updates it, imports it and destroys it: three write
// calls per test, twelve for all four, far below the sandbox limit of 200 per
// minute. They use the test data the guidelines ask for ("anonymized" texts
// and the ImmobilienScout24 office address), set no contact, so that each
// listing shows the account's default contact, and change no contact.
// TestAccListing_liveScenariosOnTheFake runs the same scenarios against the
// fake API, so that a broken configuration shows before a live run.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// liveListingTypes are the listing resource types, for the destroy check.
var liveListingTypes = map[string]*realEstateType{
	"immobilienscout24_apartment_rent": &apartmentRentKind.realEstateType,
	"immobilienscout24_apartment_buy":  &apartmentBuyKind.realEstateType,
	"immobilienscout24_house_rent":     &houseRentKind.realEstateType,
	"immobilienscout24_house_buy":      &houseBuyKind.realEstateType,
}

// liveListingScenario creates a listing of the type c with the attributes in
// create, plans again, replaces them with those in update, imports the listing
// and destroys it, each plan after an apply empty.
type liveListingScenario struct {
	c              listingCase
	create, update string
}

var (
	// The newer energy source ENVIRONMENTAL_THERMAL_ENERGY needs the query
	// parameter; the update sends three sources, which the sandbox returned in
	// another order before.
	liveApartmentRentEnergy = liveListingScenario{c: apartmentRentCase, create: `
  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "A"
  }
  construction_year           = 2015
  heating_type                = "HEAT_PUMP"
  energy_sources              = ["ENVIRONMENTAL_THERMAL_ENERGY", "ELECTRICITY"]
  building_energy_rating_type = "ENERGY_REQUIRED"
  thermal_characteristic      = 25.3
`, update: `
  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  construction_year = 1990
  heating_type      = "CENTRAL_HEATING"
  energy_sources    = ["OIL", "GAS", "SOLAR_HEATING"]
`}
	liveApartmentBuy = liveListingScenario{c: newListingCases[0], create: `
  apartment_type   = "APARTMENT"
  floor            = 2
  lift             = true
  rented           = "YES"
  service_charge   = 250.5
  built_in_kitchen = true

  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "C"
  }
  construction_year           = 1990
  heating_type                = "CENTRAL_HEATING"
  energy_sources              = ["GAS"]
  building_energy_rating_type = "ENERGY_CONSUMPTION"
  thermal_characteristic      = 95.5
`, update: `
  apartment_type = "PENTHOUSE"
  floor          = 5
  balcony        = true
  service_charge = 300
`}
	liveHouseRent = liveListingScenario{c: newListingCases[1], create: `
  building_type                   = "SINGLE_FAMILY_HOUSE"
  total_rent                      = 2200
  service_charge                  = 250
  deposit                         = "3 Kaltmieten"
  heating_costs                   = 150
  heating_costs_in_service_charge = "NO"
  pets_allowed                    = "NEGOTIABLE"

  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "C"
  }
  construction_year           = 1975
  heating_type                = "HEAT_PUMP"
  energy_sources              = ["ELECTRICITY", "ENVIRONMENTAL_THERMAL_ENERGY"]
  building_energy_rating_type = "ENERGY_REQUIRED"
  thermal_characteristic      = 80
`, update: `
  building_type    = "VILLA"
  pets_allowed     = "YES"
  built_in_kitchen = true
  free_from        = "01.12.2026"
`}
	// The update sets hot water in the energy consumption, which the sandbox
	// takes for a consumption certificate from before May 2014.
	liveHouseBuy = liveListingScenario{c: newListingCases[2], create: `
  building_type = "SEMIDETACHED_HOUSE"
  rented        = "YES"

  energy_certificate = {
    availability = "NOT_REQUIRED"
  }
  construction_year = 1935
  heating_type      = "GAS_HEATING"
  energy_sources    = ["GAS"]
`, update: `
  building_type = "VILLA"
  cellar        = "YES"

  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "BEFORE_01_MAY_2014"
  }
  construction_year                      = 1935
  building_energy_rating_type            = "ENERGY_CONSUMPTION"
  thermal_characteristic                 = 120.5
  energy_consumption_contains_warm_water = "YES"
`}
)

// liveProvider is the provider block of the live tests.
const liveProvider = "\nprovider \"immobilienscout24\" {\n  environment = \"sandbox\"\n}\n"

func TestAccApartmentRentEnergy_liveSandbox(t *testing.T) {
	testAccLivePreCheck(t)
	liveApartmentRentEnergy.run(t, liveProvider, testAccLiveCheckListingsDestroyed)
}

func TestAccApartmentBuy_liveSandbox(t *testing.T) {
	testAccLivePreCheck(t)
	liveApartmentBuy.run(t, liveProvider, testAccLiveCheckListingsDestroyed)
}

func TestAccHouseRent_liveSandbox(t *testing.T) {
	testAccLivePreCheck(t)
	liveHouseRent.run(t, liveProvider, testAccLiveCheckListingsDestroyed)
}

func TestAccHouseBuy_liveSandbox(t *testing.T) {
	testAccLivePreCheck(t)
	liveHouseBuy.run(t, liveProvider, testAccLiveCheckListingsDestroyed)
}

func TestAccListing_liveScenariosOnTheFake(t *testing.T) {
	for name, s := range map[string]liveListingScenario{"apartment_rent energy": liveApartmentRentEnergy,
		"apartment_buy": liveApartmentBuy, "house_rent": liveHouseRent, "house_buy": liveHouseBuy} {
		t.Run(name, func(t *testing.T) {
			f := newFakeAPI(t)
			s.run(t, testAccProviderBlock(f.BaseURL()), checkDestroyed(f))
		})
	}
}

// run runs the scenario with the provider block provider.
func (s liveListingScenario) run(t *testing.T, provider string, checkDestroy resource.TestCheckFunc) {
	t.Helper()
	externalID := "tf-acc-" + acctest.RandString(10)
	name := s.c.resourceName()
	config := func(title, extra string) string {
		return provider + s.c.config(title, fmt.Sprintf("  external_id = %q\n", externalID)+extra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroy,
		Steps: []resource.TestStep{
			{
				Config: config("anonymized", s.create),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(name, "id"),
					resource.TestCheckResourceAttr(name, "external_id", externalID),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: config("anonymized, updated", s.update),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(name, "title", "anonymized, updated"),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccLiveCheckListingsDestroyed asks the sandbox for every destroyed
// listing and expects 404, as testAccLiveCheckDestroyed does.
func testAccLiveCheckListingsDestroyed(s *terraform.State) error {
	client := testAccLiveClient()
	for _, rs := range s.RootModule().Resources {
		typ, ok := liveListingTypes[rs.Type]
		if !ok {
			continue
		}
		err := client.GetRealEstate(context.Background(), typ, rs.Primary.ID, &struct{}{})
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			return fmt.Errorf("checking that %s is gone: %w", rs.Primary.ID, err)
		default:
			return fmt.Errorf("%s %s can still be retrieved after destroy", rs.Type, rs.Primary.ID)
		}
	}
	return nil
}
