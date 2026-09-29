package immobilienscout24

// Acceptance tests against the fake API in fake_api_test.go. They drive the
// provider through a real Terraform (or OpenTofu) CLI, so they need TF_ACC=1,
// but no network and no credentials.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testResourceName = "immobilienscout24_apartment_rent.test"

func testAccProviderBlock(baseURL string) string {
	return fmt.Sprintf(`
provider "immobilienscout24" {
  base_url            = %q
  consumer_key        = %q
  consumer_secret     = %q
  access_token        = %q
  access_token_secret = %q
}
`, baseURL, fakeConsumerKey, fakeConsumerSecret, fakeAccessToken, fakeAccessTokenSecret)
}

// testAccApartmentRentConfig renders the resource; extra is spliced in verbatim.
func testAccApartmentRentConfig(title, baseRent, extra string) string {
	return fmt.Sprintf(`
resource "immobilienscout24_apartment_rent" "test" {
  title        = %q
  show_address = false

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  base_rent       = %s
  living_space    = 72
  number_of_rooms = 3

  courtage = {
    has_courtage = "YES"
    courtage     = "7,14%%"
  }
%s
}
`, title, baseRent, extra)
}

// captureID stores the resource id of the current state in *id.
func captureID(id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[testResourceName]
		if !ok {
			return fmt.Errorf("%s not in state", testResourceName)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func checkFakeHas(f *fakeAPI, id *string, element, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		obj, ok := f.Object(*id)
		if !ok {
			return fmt.Errorf("object %s does not exist on the fake API", *id)
		}
		c := obj.child(element)
		switch {
		case want == "" && c != nil:
			return fmt.Errorf("object %s still has <%s>%s</%s>", *id, element, c.Text, element)
		case want != "" && c == nil:
			return fmt.Errorf("object %s has no <%s>", *id, element)
		case want != "" && c.Text != want:
			return fmt.Errorf("object %s has <%s>%s</%s>, want %q", *id, element, c.Text, element, want)
		}
		return nil
	}
}

// checkDestroyed asserts that a DELETE reached the API for every object the
// provider created (except those deleted out of band) and that none is left.
func checkDestroyed(f *fakeAPI) resource.TestCheckFunc {
	return func(*terraform.State) error {
		deleted := map[string]bool{}
		for _, r := range f.Requests("DELETE") {
			deleted[strings.TrimPrefix(r.Path, fakeCollectionPath)] = true
		}
		created, outOfBand := f.History()
		if len(created) == 0 {
			return fmt.Errorf("no object was ever created")
		}
		for _, id := range created {
			if !deleted[id] && !outOfBand[id] {
				return fmt.Errorf("no DELETE request for object %s reached the API", id)
			}
		}
		if ids := f.IDs(); len(ids) > 0 {
			return fmt.Errorf("objects still exist on the fake API after destroy: %v", ids)
		}
		return nil
	}
}

func TestAccApartmentRent_lifecycle(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	provider := testAccProviderBlock(f.BaseURL())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: provider + testAccApartmentRentConfig("anonymized", "521.22", `
  description_note = "anonymized"
  floor            = 2
  lift             = true
  pets_allowed     = "NO"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&id),
					resource.TestCheckResourceAttrSet(testResourceName, "id"),
					// The API sets externalId to the scout id when none is sent.
					resource.TestCheckResourceAttrPair(testResourceName, "external_id", testResourceName, "id"),
					resource.TestCheckResourceAttr(testResourceName, "base_rent", "521.22"),
					resource.TestCheckResourceAttr(testResourceName, "living_space", "72"),
					resource.TestCheckResourceAttr(testResourceName, "floor", "2"),
					resource.TestCheckResourceAttr(testResourceName, "address.city", "Berlin"),
					checkFakeHas(f, &id, "descriptionNote", "anonymized"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Changes title and rent and drops two optional attributes.
				// PUT must carry the complete document.
				Config: provider + testAccApartmentRentConfig("anonymized, updated", "600", `
  floor        = 2
  pets_allowed = "NO"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testResourceName, "id", &id),
					resource.TestCheckResourceAttr(testResourceName, "title", "anonymized, updated"),
					resource.TestCheckNoResourceAttr(testResourceName, "description_note"),
					// Removed from the configuration, lift falls back to the API default.
					resource.TestCheckResourceAttr(testResourceName, "lift", "false"),
					checkFullPut(f, &id),
					checkFakeHas(f, &id, "title", "anonymized, updated"),
					checkFakeHas(f, &id, "descriptionNote", ""),
					checkFakeHas(f, &id, "lift", "false"),
				),
			},
			{
				ResourceName:      testResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// checkFullPut asserts that the last PUT went to the object and carried every
// element the configuration sets, not only the changed ones.
func checkFullPut(f *fakeAPI, id *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		puts := f.Requests("PUT")
		if len(puts) == 0 {
			return fmt.Errorf("no PUT request reached the API")
		}
		last := puts[len(puts)-1]
		if last.Path != fakeCollectionPath+*id {
			return fmt.Errorf("PUT went to %s, want %s", last.Path, fakeCollectionPath+*id)
		}
		for _, want := range []string{
			"<externalId>" + *id + "</externalId>", "<title>anonymized, updated</title>",
			"<street>Invalidenstrasse</street>", "<houseNumber>65</houseNumber>", "<postcode>10557</postcode>", "<city>Berlin</city>",
			"<showAddress>false</showAddress>", "<floor>2</floor>", "<baseRent>600</baseRent>", "<petsAllowed>NO</petsAllowed>",
			"<livingSpace>72</livingSpace>", "<numberOfRooms>3</numberOfRooms>", "<hasCourtage>YES</hasCourtage>", "<courtage>7,14%</courtage>",
		} {
			if !strings.Contains(last.Body, want) {
				return fmt.Errorf("PUT body lacks %s:\n%s", want, last.Body)
			}
		}
		return nil
	}
}

func TestAccApartmentRent_coordinatesAndAllAttributes(t *testing.T) {
	f := newFakeAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: testAccProviderBlock(f.BaseURL()) + `
resource "immobilienscout24_apartment_rent" "test" {
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
  description_note                = "anonymized"
  furnishing_note                 = "anonymized"
  location_note                   = "anonymized"
  other_note                      = "anonymized"
  apartment_type                  = "APARTMENT"
  floor                           = 3
  lift                            = false
  cellar                          = "YES"
  free_from                       = "sofort"
  number_of_floors                = 5
  base_rent                       = 900.5
  total_rent                      = 1200
  service_charge                  = 200.1
  deposit                         = "3 Kaltmieten"
  heating_costs                   = 99.99
  heating_costs_in_service_charge = "NO"
  pets_allowed                    = "NEGOTIABLE"
  living_space                    = 65.25
  number_of_rooms                 = 2.5
  built_in_kitchen                = true
  balcony                         = true
  garden                          = false
  courtage = {
    has_courtage  = "NO"
    courtage_note = "anonymized"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testResourceName, "external_id", "tf-acc-all"),
					resource.TestCheckResourceAttr(testResourceName, "address.coordinates.latitude", "52.53"),
					resource.TestCheckResourceAttr(testResourceName, "number_of_rooms", "2.5"),
					resource.TestCheckResourceAttr(testResourceName, "garden", "false"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      testResourceName,
				ImportState:       true,
				ImportStateVerify: true,
				// Import cannot tell configured coordinates from geocoded ones, so
				// it leaves them unmanaged; the next plan adds them back.
				ImportStateVerifyIgnore: []string{"address.coordinates"},
			},
		},
	})
}

func TestAccApartmentRent_deletedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var first, second string
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&first)},
			{
				PreConfig: func() { f.DeleteOutOfBand(first) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionCreate)},
				},
				Check: resource.ComposeTestCheckFunc(
					captureID(&second),
					func(*terraform.State) error {
						if second == first {
							return fmt.Errorf("expected a new object, id is still %s", first)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccApartmentRent_changedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&id)},
			{
				PreConfig: func() {
					f.SetOutOfBand(id, "title", "edited on the website")
					f.SetOutOfBand(id, "baseRent", "999")
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectNonEmptyPlan(),
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					checkFakeHas(f, &id, "title", "anonymized"),
					checkFakeHas(f, &id, "baseRent", "521.22"),
				),
			},
		},
	})
}

func TestAccApartmentRent_credentialsFromEnvironment(t *testing.T) {
	f := newFakeAPI(t)
	t.Setenv("IMMOBILIENSCOUT24_CONSUMER_KEY", fakeConsumerKey)
	t.Setenv("IMMOBILIENSCOUT24_CONSUMER_SECRET", fakeConsumerSecret)
	t.Setenv("IMMOBILIENSCOUT24_ACCESS_TOKEN", fakeAccessToken)
	t.Setenv("IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET", fakeAccessTokenSecret)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf("provider \"immobilienscout24\" {\n  base_url = %q\n}\n", f.BaseURL()) +
				testAccApartmentRentConfig("anonymized", "521.22", ""),
			Check: resource.TestCheckResourceAttrSet(testResourceName, "id"),
		}},
	})
}

// The API may return text in another letter case or without the trailing
// newline of a heredoc. Neither may surface as a difference.
func TestAccApartmentRent_serverNormalisesText(t *testing.T) {
	f := newFakeAPI(t)
	f.lowercaseAddress = true
	extra := `
  description_note = <<-EOT
    anonymized
  EOT
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{{
			Config: testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", extra),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr(testResourceName, "address.street", "Invalidenstrasse"),
				resource.TestCheckResourceAttr(testResourceName, "address.city", "Berlin"),
				resource.TestCheckResourceAttr(testResourceName, "description_note", "anonymized\n"),
			),
		}},
	})
}

// Import takes the numeric scout id only. An ext-<externalId> id would be kept
// as the resource id and stop resolving once external_id changes.
func TestAccApartmentRent_importRejectsNonNumericID(t *testing.T) {
	f := newFakeAPI(t)
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:        config,
				ResourceName:  testResourceName,
				ImportState:   true,
				ImportStateId: "ext-anonymized",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}
