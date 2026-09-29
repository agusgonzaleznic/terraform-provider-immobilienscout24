package immobilienscout24

// Acceptance tests of the contact_id of immobilienscout24_apartment_rent
// against the fake API.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// checkListingContact asserts the contact of a listing on the fake API.
func checkListingContact(f *fakeAPI, apartmentID, contactID *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := f.ListingContact(*apartmentID); got != *contactID {
			return fmt.Errorf("listing %s has contact %s on the fake API, want %s", *apartmentID, got, *contactID)
		}
		return nil
	}
}

// checkLastRealEstateWrite asserts that the body of the last POST or PUT of a
// real estate contains snippet(), or lacks it when present is false.
func checkLastRealEstateWrite(f *fakeAPI, method string, snippet func() string, present bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		requests := f.Requests(method)
		for i := len(requests) - 1; i >= 0; i-- {
			if !strings.HasPrefix(requests[i].Path, fakeCollectionPath) {
				continue
			}
			if strings.Contains(requests[i].Body, snippet()) != present {
				return fmt.Errorf("%s body (want %s present: %v):\n%s", method, snippet(), present, requests[i].Body)
			}
			return nil
		}
		return fmt.Errorf("no %s request for a real estate reached the API", method)
	}
}

// contactElement is the contact element as encoding/xml writes it.
func contactElement(id *string) func() string {
	return func() string { return `<contact id="` + *id + `"></contact>` }
}

func TestAccApartmentRent_contact(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID, x, y string
	config := func(contact string) string {
		return testAccProviderBlock(f.BaseURL()) + testAccContactConfig("x", "") + testAccContactConfig("y", "") +
			testAccApartmentRentConfig("anonymized", "521.22", "  contact_id = immobilienscout24_contact."+contact+".id\n")
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeAggregateTestCheckFunc(checkDestroyed(f), checkContactsDestroyed(f)),
		Steps: []resource.TestStep{
			{
				Config: config("x"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&apartmentID),
					captureResourceID("immobilienscout24_contact.x", &x),
					captureResourceID("immobilienscout24_contact.y", &y),
					resource.TestCheckResourceAttrPair(testResourceName, "contact_id", "immobilienscout24_contact.x", "id"),
					checkListingContact(f, &apartmentID, &x),
					// Right after showAddress, the position in the XSD, which the fake also checks.
					checkLastRealEstateWrite(f, "POST", func() string { return "<showAddress>false</showAddress>" + contactElement(&x)() }, true),
					func(*terraform.State) error {
						if body := f.RenderContact(x); !strings.Contains(body, "<realEstateReferenceCount>1</realEstateReferenceCount>") {
							return fmt.Errorf("contact %s does not count the listing:\n%s", x, body)
						}
						return nil
					},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: config("y"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("immobilienscout24_contact.x", plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction("immobilienscout24_contact.y", plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testResourceName, "id", &apartmentID),
					resource.TestCheckResourceAttrPtr(testResourceName, "contact_id", &y),
					checkListingContact(f, &apartmentID, &y),
					checkLastRealEstateWrite(f, "PUT", contactElement(&y), true),
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

// Regression test: a listing PUT without a contact resets the listing to the
// default contact (observed on the sandbox, 2026-09-29). Up to v0.1.0 the
// provider sent none, so every update of an apartment lost a contact that had
// been chosen on the website.
func TestAccApartmentRent_keepsContactChosenOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID, websiteContact string
	seed := fakeDefaultContactID
	provider := testAccProviderBlock(f.BaseURL())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{
				// Without contact_id, a new listing gets the default contact.
				Config: provider + testAccApartmentRentConfig("anonymized", "521.22", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&apartmentID),
					resource.TestCheckResourceAttr(testResourceName, "contact_id", seed),
					checkListingContact(f, &apartmentID, &seed),
					checkLastRealEstateWrite(f, "POST", func() string { return "<contact" }, false),
				),
			},
			{
				PreConfig: func() {
					websiteContact = f.AddContactOutOfBand("tf-acc-website@is24-test.de", "tf-acc-website")
					f.SetListingContactOutOfBand(apartmentID, websiteContact)
				},
				Config: provider + testAccApartmentRentConfig("anonymized, updated", "521.22", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkFakeHas(f, &apartmentID, "title", "anonymized, updated"),
					checkListingContact(f, &apartmentID, &websiteContact),
					resource.TestCheckResourceAttrPtr(testResourceName, "contact_id", &websiteContact),
					checkLastRealEstateWrite(f, "PUT", contactElement(&websiteContact), true),
				),
			},
		},
	})
}

// Deleting a contact moves its listings to the default contact. The plan
// then creates the contact anew and points the listing at it.
func TestAccApartmentRent_contactDeletedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID, first, second string
	seed := fakeDefaultContactID
	config := testAccProviderBlock(f.BaseURL()) + testAccContactConfig("x", "") +
		testAccApartmentRentConfig("anonymized", "521.22", "  contact_id = immobilienscout24_contact.x.id\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeAggregateTestCheckFunc(checkDestroyed(f), checkContactsDestroyed(f)),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&apartmentID),
					captureResourceID("immobilienscout24_contact.x", &first),
					checkListingContact(f, &apartmentID, &first),
				),
			},
			{
				PreConfig: func() {
					f.DeleteContactOutOfBand(first)
					if got := f.ListingContact(apartmentID); got != seed {
						t.Fatalf("after deleting its contact, the listing has contact %s, want the default %s", got, seed)
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("immobilienscout24_contact.x", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID("immobilienscout24_contact.x", &second),
					func(*terraform.State) error {
						if second == first {
							return fmt.Errorf("expected a new contact, id is still %s", first)
						}
						return nil
					},
					checkListingContact(f, &apartmentID, &second),
					resource.TestCheckResourceAttrPair(testResourceName, "contact_id", "immobilienscout24_contact.x", "id"),
				),
			},
		},
	})
}

func TestAccApartmentRent_invalidContactID(t *testing.T) {
	f := newFakeAPI(t)
	var steps []resource.TestStep
	for _, id := range []string{"0124308575", "ext-tf-acc", "-1"} {
		steps = append(steps, resource.TestStep{
			Config: testAccProviderBlock(f.BaseURL()) +
				testAccApartmentRentConfig("anonymized", "521.22", fmt.Sprintf("  contact_id = %q\n", id)),
			ExpectError: wrapped("contact_id must be a whole number in digits, without a leading zero"),
		})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps:                    steps,
	})
}

// Removing a contact together with the contact_id that names it, while the
// listing changes too, works in one apply. Terraform deletes the contact
// first, the API moves the listing to the default contact, and the update
// must not name the deleted contact, which the API refuses with 412.
func TestAccApartmentRent_contactRemovedWithItsReference(t *testing.T) {
	f := newFakeAPI(t)
	var apartmentID, x string
	seed := fakeDefaultContactID
	provider := testAccProviderBlock(f.BaseURL())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeTestCheckFunc(checkDestroyed(f), checkContactsDestroyed(f)),
		Steps: []resource.TestStep{
			{
				Config: provider + testAccContactConfig("x", "") +
					testAccApartmentRentConfig("anonymized", "521.22", "  contact_id = immobilienscout24_contact.x.id\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&apartmentID),
					captureResourceID("immobilienscout24_contact.x", &x),
					checkListingContact(f, &apartmentID, &x),
				),
			},
			{
				Config: provider + testAccApartmentRentConfig("anonymized, updated", "521.22", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("immobilienscout24_contact.x", plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testResourceName, "title", "anonymized, updated"),
					resource.TestCheckResourceAttr(testResourceName, "contact_id", seed),
					checkListingContact(f, &apartmentID, &seed),
				),
			},
		},
	})
}
