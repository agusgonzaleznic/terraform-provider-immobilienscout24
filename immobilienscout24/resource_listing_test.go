package immobilienscout24

// Acceptance tests of the listing resources besides apartment rent, whose own
// tests are in resource_apartment_rent_test.go, against the fake API. Each
// test runs for every one of the three types; the energy tests, which run for
// all four, are in resource_listing_energy_test.go.

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// listingCase is a listing resource type in the tests that every type runs.
type listingCase struct {
	name     string // the resource type after the provider's, such as "apartment_buy"
	root     string // the XSD root element
	required string // the attributes it requires besides title, show_address, address and courtage, in HCL
	// created are elements the create of the required attributes must send.
	created []string
	// defaults are the attributes the API fills in when they are left out.
	defaults map[string]string
	// all sets every other attribute of its own; allChecks are some of them.
	all       string
	allChecks map[string]string
	// update changes attributes of its own, and updated are elements the PUT
	// must then send.
	update  string
	updated []string
}

func (c listingCase) resourceName() string { return "immobilienscout24_" + c.name + ".test" }

// config renders the resource with the attributes it requires; extra is
// spliced in verbatim.
func (c listingCase) config(title, extra string) string {
	return c.configNamed("test", title, extra)
}

// configNamed is config for a resource named label.
func (c listingCase) configNamed(label, title, extra string) string {
	return fmt.Sprintf(`
resource "immobilienscout24_%s" %q {
  title        = %q
  show_address = false

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  courtage = {
    has_courtage = "YES"
    courtage     = "7,14%%"
  }
%s%s
}
`, c.name, label, title, c.required, extra)
}

// fullConfig renders the resource with every attribute set.
func (c listingCase) fullConfig() string {
	return fmt.Sprintf(`
resource "immobilienscout24_%s" "test" {
  external_id  = "tf-acc-all"
  title        = "anonymized"
  show_address = true

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
    coordinates = {
      latitude  = 52.53
      longitude = 13.38
    }
  }

  description_note = "anonymized"
  furnishing_note  = "anonymized"
  location_note    = "anonymized"
  other_note       = "anonymized"
  cellar           = "YES"
  free_from        = "sofort"
  number_of_floors = 3

  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "C"
  }
  construction_year                      = 1990
  heating_type                           = "CENTRAL_HEATING"
  energy_sources                         = ["GAS", "SOLAR_HEATING"]
  building_energy_rating_type            = "ENERGY_CONSUMPTION"
  thermal_characteristic                 = 95.5
  energy_consumption_contains_warm_water = "NOT_APPLICABLE"

  courtage = {
    has_courtage  = "NO"
    courtage_note = "anonymized"
  }
%s%s
}
`, c.name, c.required, c.all)
}

var (
	apartmentRentCase = listingCase{name: "apartment_rent", root: "apartmentRent", required: `
  base_rent       = 521.22
  living_space    = 72
  number_of_rooms = 3
`}
	newListingCases = []listingCase{
		{
			name: "apartment_buy", root: "apartmentBuy", required: `
  purchase_price  = 99000
  living_space    = 50
  number_of_rooms = 2
`,
			created: []string{"<price><value>99000</value><currency>EUR</currency></price>"},
			defaults: map[string]string{"apartment_type": "NO_INFORMATION", "lift": "false", "cellar": "NOT_APPLICABLE",
				"rented": "NOT_APPLICABLE", "built_in_kitchen": "false", "balcony": "false", "garden": "false"},
			all: `
  apartment_type   = "APARTMENT"
  floor            = 2
  lift             = true
  rented           = "YES"
  built_in_kitchen = true
  balcony          = true
  garden           = true
  service_charge   = 250.5
`,
			allChecks: map[string]string{"floor": "2", "rented": "YES", "service_charge": "250.5", "purchase_price": "99000"},
			update: `
  apartment_type = "PENTHOUSE"
  floor          = 5
  rented         = "YES"
  service_charge = 300
  free_from      = "01.12.2026"
`,
			updated: []string{"<apartmentType>PENTHOUSE</apartmentType>", "<floor>5</floor>", "<rented>YES</rented>",
				"<price><value>99000</value><currency>EUR</currency></price>", "<serviceCharge>300</serviceCharge>",
				"<freeFrom>01.12.2026</freeFrom>", "<livingSpace>50</livingSpace>", "<numberOfRooms>2</numberOfRooms>"},
		},
		{
			name: "house_rent", root: "houseRent", required: `
  living_space    = 180.27
  plot_area       = 125.72
  number_of_rooms = 5
  base_rent       = 986.17
`,
			// buildingType is required by the XSD, so its default is sent.
			created: []string{"<buildingType>NO_INFORMATION</buildingType>", "<plotArea>125.72</plotArea>"},
			defaults: map[string]string{"building_type": "NO_INFORMATION", "cellar": "NOT_APPLICABLE",
				"heating_costs_in_service_charge": "NOT_APPLICABLE", "pets_allowed": "NO_INFORMATION", "built_in_kitchen": "false"},
			all: `
  building_type                   = "SINGLE_FAMILY_HOUSE"
  total_rent                      = 2200
  service_charge                  = 250
  deposit                         = "3 Kaltmieten"
  heating_costs                   = 150
  heating_costs_in_service_charge = "NO"
  pets_allowed                    = "NEGOTIABLE"
  built_in_kitchen                = true
`,
			allChecks: map[string]string{"building_type": "SINGLE_FAMILY_HOUSE", "heating_costs": "150", "plot_area": "125.72"},
			update: `
  building_type    = "VILLA"
  total_rent       = 1200
  pets_allowed     = "YES"
  built_in_kitchen = true
  free_from        = "01.12.2026"
`,
			updated: []string{"<buildingType>VILLA</buildingType>", "<totalRent>1200</totalRent>", "<petsAllowed>YES</petsAllowed>",
				"<builtInKitchen>true</builtInKitchen>", "<freeFrom>01.12.2026</freeFrom>", "<baseRent>986.17</baseRent>",
				"<plotArea>125.72</plotArea>", "<numberOfRooms>5</numberOfRooms>"},
		},
		{
			name: "house_buy", root: "houseBuy", required: `
  purchase_price  = 750000
  living_space    = 160
  plot_area       = 450
  number_of_rooms = 6
`,
			created: []string{"<buildingType>NO_INFORMATION</buildingType>",
				"<price><value>750000</value><currency>EUR</currency></price>"},
			defaults: map[string]string{"building_type": "NO_INFORMATION", "cellar": "NOT_APPLICABLE", "rented": "NOT_APPLICABLE"},
			all: `
  building_type = "SEMIDETACHED_HOUSE"
  rented        = "YES"
`,
			allChecks: map[string]string{"building_type": "SEMIDETACHED_HOUSE", "rented": "YES", "plot_area": "450"},
			update: `
  building_type    = "CASTLE_MANOR_HOUSE"
  rented           = "YES"
  number_of_floors = 3
  free_from        = "nach Vereinbarung"
`,
			updated: []string{"<buildingType>CASTLE_MANOR_HOUSE</buildingType>", "<rented>YES</rented>",
				"<numberOfFloors>3</numberOfFloors>", "<freeFrom>nach Vereinbarung</freeFrom>",
				"<price><value>750000</value><currency>EUR</currency></price>"},
		},
	}
)

// written returns a check that the last real estate write of method carries
// every snippet.
func written(f *fakeAPI, method string, snippets []string) resource.TestCheckFunc {
	var checks []resource.TestCheckFunc
	for _, s := range snippets {
		checks = append(checks, checkLastRealEstateWrite(f, method, func() string { return s }, true))
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
}

// Create with the required attributes only, which leaves no difference behind,
// then update several attributes with a full PUT, then import.
func TestAccListing_lifecycle(t *testing.T) {
	for _, c := range newListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			var id string
			name, provider := c.resourceName(), testAccProviderBlock(f.BaseURL())
			checks := []resource.TestCheckFunc{
				captureResourceID(name, &id),
				resource.TestCheckResourceAttrPair(name, "external_id", name, "id"),
				resource.TestCheckResourceAttr(name, "energy_sources.#", "1"),
				resource.TestCheckTypeSetElemAttr(name, "energy_sources.*", "NO_INFORMATION"),
				resource.TestCheckResourceAttr(name, "energy_consumption_contains_warm_water", "NOT_APPLICABLE"),
				resource.TestCheckNoResourceAttr(name, "energy_certificate"),
				checkLastRealEstateWrite(f, "POST", func() string { return "<realestates:" + c.root + " " }, true),
				written(f, "POST", c.created),
			}
			for attribute, value := range c.defaults {
				checks = append(checks, resource.TestCheckResourceAttr(name, attribute, value))
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkDestroyed(f),
				Steps: []resource.TestStep{
					{
						Config: provider + c.config("anonymized", ""),
						Check:  resource.ComposeAggregateTestCheckFunc(checks...),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
					{
						Config: provider + c.config("anonymized, updated", c.update),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttrPtr(name, "id", &id),
							resource.TestCheckResourceAttr(name, "title", "anonymized, updated"),
							checkFakeHas(f, &id, "title", "anonymized, updated"),
							written(f, "PUT", append([]string{"<title>anonymized, updated</title>", "<externalId>"}, c.updated...)),
						),
					},
					{
						ResourceName:      name,
						ImportState:       true,
						ImportStateVerify: true,
					},
				},
			})
		})
	}
}

func TestAccListing_allAttributes(t *testing.T) {
	for _, c := range newListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			name := c.resourceName()
			checks := []resource.TestCheckFunc{
				resource.TestCheckResourceAttr(name, "external_id", "tf-acc-all"),
				resource.TestCheckResourceAttr(name, "address.coordinates.latitude", "52.53"),
				resource.TestCheckResourceAttr(name, "energy_certificate.efficiency_class", "C"),
				resource.TestCheckResourceAttr(name, "energy_sources.#", "2"),
				resource.TestCheckResourceAttr(name, "thermal_characteristic", "95.5"),
				resource.TestCheckResourceAttr(name, "courtage.courtage_note", "anonymized"),
			}
			for attribute, value := range c.allChecks {
				checks = append(checks, resource.TestCheckResourceAttr(name, attribute, value))
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkDestroyed(f),
				Steps: []resource.TestStep{
					{
						Config: testAccProviderBlock(f.BaseURL()) + c.fullConfig(),
						Check:  resource.ComposeAggregateTestCheckFunc(checks...),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
					{
						ResourceName:      name,
						ImportState:       true,
						ImportStateVerify: true,
						// Import cannot tell configured coordinates from geocoded ones.
						ImportStateVerifyIgnore: []string{"address.coordinates"},
					},
				},
			})
		})
	}
}

// An update without contact_id sends the contact the listing has, also one
// chosen on the website; the API resets a listing to the default contact on a
// PUT without one.
func TestAccListing_keepsContactChosenOutsideTerraform(t *testing.T) {
	for _, c := range newListingCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			var id, websiteContact string
			seed := fakeDefaultContactID
			name, provider := c.resourceName(), testAccProviderBlock(f.BaseURL())
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkDestroyed(f),
				Steps: []resource.TestStep{
					{
						Config: provider + c.config("anonymized", ""),
						Check: resource.ComposeAggregateTestCheckFunc(
							captureResourceID(name, &id),
							resource.TestCheckResourceAttr(name, "contact_id", seed),
							checkListingContact(f, &id, &seed),
							checkLastRealEstateWrite(f, "POST", func() string { return "<contact" }, false),
						),
					},
					{
						PreConfig: func() {
							websiteContact = f.AddContactOutOfBand("tf-acc-website@is24-test.de", "tf-acc-website")
							f.SetListingContactOutOfBand(id, websiteContact)
						},
						Config: provider + c.config("anonymized, updated", ""),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							checkFakeHas(f, &id, "title", "anonymized, updated"),
							checkListingContact(f, &id, &websiteContact),
							resource.TestCheckResourceAttrPtr(name, "contact_id", &websiteContact),
							checkLastRealEstateWrite(f, "PUT", contactElement(&websiteContact), true),
						),
					},
				},
			})
		})
	}
}

// Importing a listing of another type fails and names both types.
func TestAccListing_importOtherType(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	apartment, house := newListingCases[0], newListingCases[2]
	config := testAccProviderBlock(f.BaseURL()) + apartment.config("anonymized", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if ids := f.IDs(); len(ids) > 0 {
				return fmt.Errorf("objects still exist on the fake API after destroy: %v", ids)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  captureResourceID(apartment.resourceName(), &id),
			},
			{
				Config:            config + house.configNamed("other", "anonymized", ""),
				ResourceName:      "immobilienscout24_house_buy.other",
				ImportState:       true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return id, nil },
				ExpectError: wrapped("Error reading house for sale",
					`expected a realestates:houseBuy, the API returned <apartmentBuy>`, "this resource only manages houses for sale"),
			},
		},
	})
	if created, _ := f.History(); !slices.Contains(created, id) || len(created) != 1 {
		t.Errorf("objects created: %v, want only the apartment %s", created, id)
	}
}

// A purchase price of 0 is taken on create and update (observed 2026-09-30);
// the listing then shows "Preis auf Anfrage".
func TestAccListing_purchasePriceZero(t *testing.T) {
	for _, c := range []listingCase{newListingCases[0], newListingCases[2]} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAPI(t)
			name, provider := c.resourceName(), testAccProviderBlock(f.BaseURL())
			price := regexp.MustCompile(`purchase_price  = \d+`)
			zero := price.ReplaceAllString(c.config("anonymized", ""), "purchase_price  = 0")
			sentZero := func() string { return "<price><value>0</value><currency>EUR</currency></price>" }
			check := func(method string) resource.TestCheckFunc {
				return resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "purchase_price", "0"),
					checkLastRealEstateWrite(f, method, sentZero, true),
				)
			}
			empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             checkDestroyed(f),
				Steps: []resource.TestStep{
					{Config: provider + zero, Check: check("POST"), ConfigPlanChecks: empty},
					{Config: provider + c.config("anonymized", ""), ConfigPlanChecks: empty},
					{Config: provider + zero, Check: check("PUT"), ConfigPlanChecks: empty},
				},
			})
		})
	}
}
