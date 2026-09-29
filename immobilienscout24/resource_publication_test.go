package immobilienscout24

// Acceptance tests of immobilienscout24_publication against the fake API. Like
// the apartment tests, they need TF_ACC=1 and a Terraform or OpenTofu CLI, but
// no network and no credentials.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	testPortalName   = "immobilienscout24_publication.portal"
	testHomepageName = "immobilienscout24_publication.homepage"
)

// testAccPublicationConfig renders a publication of the test apartment.
func testAccPublicationConfig(name, channelID string) string {
	return fmt.Sprintf(`
resource "immobilienscout24_publication" %q {
  real_estate_id = immobilienscout24_apartment_rent.test.id
  channel_id     = %q
}
`, name, channelID)
}

// checkPublication asserts the state of a publication of the apartment.
func checkPublication(name string, apartmentID *string, channelID string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(name, "id", *apartmentID+"_"+channelID),
			resource.TestCheckResourceAttr(name, "real_estate_id", *apartmentID),
			resource.TestCheckResourceAttr(name, "channel_id", channelID),
		)(s)
	}
}

// checkFakePublished asserts that the fake API holds exactly these
// publications of the apartment and no other publication.
func checkFakePublished(f *fakeAPI, apartmentID *string, channels ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		var want []string
		for _, c := range channels {
			want = append(want, *apartmentID+"_"+c)
		}
		if got := f.PublicationIDs(); !slices.Equal(got, want) {
			return fmt.Errorf("publications on the fake API = %v, want %v", got, want)
		}
		return nil
	}
}

// checkFakeRenders asserts that the fake API's GET of the real estate, which
// the apartment resource reads, contains every snippet.
func checkFakeRenders(f *fakeAPI, id *string, snippets ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		obj, ok := f.Object(*id)
		if !ok {
			return fmt.Errorf("object %s does not exist on the fake API", *id)
		}
		body := f.render(*id, obj)
		for _, s := range snippets {
			if !strings.Contains(body, s) {
				return fmt.Errorf("GET of real estate %s lacks %s:\n%s", *id, s, body)
			}
		}
		return nil
	}
}

// checkPublicationsDestroyed asserts that a DELETE reached the API for every
// publication the provider created, except those removed out of band, and
// that no publication is left.
func checkPublicationsDestroyed(f *fakeAPI) resource.TestCheckFunc {
	return func(*terraform.State) error {
		removals := map[string]int{}
		for _, r := range f.Requests("DELETE") {
			if id, ok := strings.CutPrefix(r.Path, fakePublishPath+"/"); ok {
				removals[id]++
			}
		}
		published, removedOutOfBand := f.PublishHistory()
		for _, id := range removedOutOfBand {
			removals[id]++
		}
		for _, id := range published {
			if removals[id] == 0 {
				return fmt.Errorf("no DELETE request for publication %s reached the API", id)
			}
			removals[id]--
		}
		if ids := f.PublicationIDs(); len(ids) > 0 {
			return fmt.Errorf("publications still exist on the fake API after destroy: %v", ids)
		}
		return nil
	}
}

func testAccPublicationCheckDestroy(f *fakeAPI) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(checkDestroyed(f), checkPublicationsDestroyed(f))
}

// Terraform creates both publications in parallel once the apartment exists,
// and the fake API fails publish requests that overlap, so this test also
// proves that the provider sends them one at a time.
func TestAccPublication_lifecycle(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID string
	config := func(title string, publications ...string) string {
		return testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig(title, "521.22", "") + strings.Join(publications, "")
	}
	portal := testAccPublicationConfig("portal", "10000")
	homepage := testAccPublicationConfig("homepage", "10001")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: config("anonymized", portal, homepage),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&apartmentID),
					checkPublication(testPortalName, &apartmentID, "10000"),
					checkPublication(testHomepageName, &apartmentID, "10001"),
					checkFakePublished(f, &apartmentID, "10000", "10001"),
					// The apartment resource reads both elements and must ignore them.
					checkFakeRenders(f, &apartmentID, "<realEstateState>ACTIVE</realEstateState>",
						`<publishChannel id="10000" title="ImmobilienScout24">`, `<publishChannel id="10001" title="Homepage">`),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      testHomepageName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      testResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// A full PUT of the apartment keeps its publications, as on the sandbox.
				Config: config("anonymized, updated", portal, homepage),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(testHomepageName, plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkFakeHas(f, &apartmentID, "title", "anonymized, updated"),
					checkFakePublished(f, &apartmentID, "10000", "10001"),
					checkFakeRenders(f, &apartmentID, "<realEstateState>ACTIVE</realEstateState>"),
				),
			},
			{
				// Unpublishing from 10000 deactivates the listing, although it
				// is still published on 10001.
				Config: config("anonymized, updated", homepage),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionDestroy)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkFakePublished(f, &apartmentID, "10001"),
					checkFakeRenders(f, &apartmentID, "<realEstateState>INACTIVE</realEstateState>"),
				),
			},
			{
				Config: config("anonymized, updated", testAccPublicationConfig("homepage", "10000")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testHomepageName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkPublication(testHomepageName, &apartmentID, "10000"),
					checkFakePublished(f, &apartmentID, "10000"),
				),
			},
		},
	})
}

func TestAccPublication_movesToAnotherListing(t *testing.T) {
	f := newFakeAPI(t)
	var otherID string
	apartments := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		strings.Replace(testAccApartmentRentConfig("anonymized", "600", ""), `"test"`, `"other"`, 1)
	moved := strings.Replace(testAccPublicationConfig("portal", "10000"), "apartment_rent.test.", "apartment_rent.other.", 1)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: apartments + testAccPublicationConfig("portal", "10000"),
				Check: func(s *terraform.State) error {
					otherID = s.RootModule().Resources["immobilienscout24_apartment_rent.other"].Primary.ID
					return nil
				},
			},
			{
				Config: apartments + moved,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkPublication(testPortalName, &otherID, "10000"),
					checkFakePublished(f, &otherID, "10000"),
				),
			},
		},
	})
}

func TestAccPublication_unpublishedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID string
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccPublicationConfig("portal", "10000")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&apartmentID)},
			{
				PreConfig: func() { f.UnpublishOutOfBand(apartmentID + "_10000") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionCreate),
					},
				},
				Check: checkFakePublished(f, &apartmentID, "10000"),
			},
		},
	})
}

// Deleting a real estate deletes its publications, so both are created anew.
func TestAccPublication_apartmentDeletedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var first, second string
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccPublicationConfig("portal", "10000")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&first)},
			{
				PreConfig: func() { f.DeleteOutOfBand(first) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&second),
					func(*terraform.State) error {
						if second == first {
							return fmt.Errorf("expected a new object, id is still %s", first)
						}
						return nil
					},
					checkPublication(testPortalName, &second, "10000"),
					checkFakePublished(f, &second, "10000"),
				),
			},
		},
	})
}

// A listing that is already published on the channel makes Create fail with
// the import command, and that command then works.
func TestAccPublication_alreadyPublished(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID string
	apartment := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	config := apartment + testAccPublicationConfig("portal", "10000")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: apartment, Check: captureID(&apartmentID)},
			{
				// The first object of a new fake API is 315000001.
				PreConfig: func() { f.PublishOutOfBand(apartmentID, "10000") },
				Config:    config,
				ExpectError: regexp.MustCompile(`Listing is already published on this channel[\s\S]*` +
					regexp.QuoteMeta("terraform import immobilienscout24_publication.<name> 315000001_10000")),
			},
			{
				Config:             config,
				ResourceName:       testPortalName,
				ImportState:        true,
				ImportStateId:      "315000001_10000",
				ImportStatePersist: true,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkPublication(testPortalName, &apartmentID, "10000"),
			},
		},
	})
}

// The API also answers 409 to concurrent access. Without a publication to
// import, the error must say to retry rather than suggest an import that fails.
func TestAccPublication_conflictWithoutPublication(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID string
	apartment := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	config := apartment + testAccPublicationConfig("portal", "10000")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: apartment, Check: captureID(&apartmentID)},
			{
				PreConfig:   f.ConflictNextPublish,
				Config:      config,
				ExpectError: regexp.MustCompile(`Publish request conflicted[\s\S]*Retry the apply`),
			},
			{
				Config: config,
				Check:  checkPublication(testPortalName, &apartmentID, "10000"),
			},
		},
	})
}

func TestAccPublication_invalidArguments(t *testing.T) {
	f := newFakeAPI(t)
	publication := func(realEstateID, channelID string) string {
		return testAccProviderBlock(f.BaseURL()) + fmt.Sprintf(`
resource "immobilienscout24_publication" "portal" {
  real_estate_id = %q
  channel_id     = %q
}
`, realEstateID, channelID)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkPublicationsDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config:      publication("315000001", "010000"),
				ExpectError: regexp.MustCompile(`channel_id must be a whole number in digits`),
			},
			{
				// A leading zero would publish under another id, since the API
				// reads ids as numbers.
				Config:      publication("0315000001", "10000"),
				ExpectError: regexp.MustCompile(`real_estate_id must be a whole number in digits`),
			},
			{
				Config:      publication("ext-anonymized", "10000"),
				ExpectError: regexp.MustCompile(`real_estate_id must be a whole number in digits`),
			},
			{
				Config:      publication("1", "10000"),
				ExpectError: regexp.MustCompile(`Real estate 1 does not exist`),
			},
		},
	})
}

func TestAccPublication_importRejectsInvalidID(t *testing.T) {
	f := newFakeAPI(t)
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccPublicationConfig("portal", "10000")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccPublicationCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:        config,
				ResourceName:  testPortalName,
				ImportState:   true,
				ImportStateId: "315000001",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}
