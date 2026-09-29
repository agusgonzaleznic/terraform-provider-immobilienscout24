package immobilienscout24

// Acceptance tests of the default contact rules of immobilienscout24_contact
// against the fake API. Like the other acceptance tests, they need TF_ACC=1 and
// a Terraform or OpenTofu CLI, but no network and no credentials.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// checkFakeDefault asserts which contact the fake API holds as the default.
func checkFakeDefault(f *fakeAPI, id *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := f.DefaultContactID(); got != *id {
			return fmt.Errorf("the default contact on the fake API is %s, want %s", got, *id)
		}
		return nil
	}
}

// default_contact = true moves the default; the default contact cannot be
// destroyed; making another contact the default lets it go.
func init() {
	// Keep the tests where the API refuses to delete the default contact fast.
	// Moving the default to a contact created in the same apply needs only one
	// retry against the fake API.
	defaultContactDeleteWait = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
}

func TestAccContact_defaultContact(t *testing.T) {
	f := newFakeAPI(t)
	var id, otherID string
	seed := fakeDefaultContactID
	provider := testAccProviderBlock(f.BaseURL())
	makeDefault := "  default_contact = true\n"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: provider + testAccContactConfig("test", makeDefault),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testContactName, &id),
					resource.TestCheckResourceAttr(testContactName, "default_contact", "true"),
					checkFakeDefault(f, &id),
					checkContactWrite(f, "POST", nil, []string{"<defaultContact>true</defaultContact>"}, nil),
					func(*terraform.State) error {
						if body := f.RenderContact(seed); !strings.Contains(body, "<defaultContact>false</defaultContact>") {
							return fmt.Errorf("the previous default contact is still the default:\n%s", body)
						}
						return nil
					},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config:  provider + testAccContactConfig("test", makeDefault),
				Destroy: true,
				ExpectError: wrapped("Cannot delete the default contact",
					"is the account's default contact, which ImmobilienScout24 refuses to delete",
					"set default_contact = true on another immobilienscout24_contact", "then destroy again",
					"default contact can not be deleted"),
			},
			{
				// Another contact takes the default. The first one is not
				// updated, and its next refresh reads the new flag.
				Config: provider + testAccContactConfig("test", "") + testAccContactConfig("other", makeDefault),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(testContactOther, plancheck.ResourceActionCreate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testContactOther, &otherID),
					checkFakeDefault(f, &otherID),
				),
			},
			{
				// Now the first contact can be destroyed.
				Config: provider + testAccContactConfig("other", makeDefault),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionDestroy)},
				},
				Check: func(*terraform.State) error {
					if slices.Contains(f.ContactIDs(), id) {
						return fmt.Errorf("contact %s still exists", id)
					}
					return nil
				},
			},
			{
				// The default goes back to the account's original contact, as
				// if chosen on the website, so that the last contact can be
				// destroyed too. Read picks up the flag without a diff.
				PreConfig: func() { f.MakeDefaultOutOfBand(seed) },
				Config:    provider + testAccContactConfig("other", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(testContactOther, "default_contact", "false"),
			},
		},
	})
}

// When another contact takes the default during the apply that updates the
// default contact, the update must not report an inconsistent result: the
// flag of the default contact is only known after apply. depends_on makes
// the other contact's creation run first.
func TestAccContact_defaultMovesDuringUpdate(t *testing.T) {
	f := newFakeAPI(t)
	var id, otherID string
	provider := testAccProviderBlock(f.BaseURL())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: provider + testAccContactConfig("test", "  default_contact = true\n"),
				Check:  resource.ComposeAggregateTestCheckFunc(captureResourceID(testContactName, &id), checkFakeDefault(f, &id)),
			},
			{
				Config: provider + testAccContactConfig("test", `  firstname  = "anonymized"
  depends_on = [immobilienscout24_contact.other]
`) + testAccContactConfig("other", "  default_contact = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue(testContactName, tfjsonpath.New("default_contact")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testContactOther, &otherID),
					resource.TestCheckResourceAttr(testContactName, "default_contact", "false"),
					resource.TestCheckResourceAttr(testContactOther, "default_contact", "true"),
					checkFakeDefault(f, &otherID),
				),
			},
			{
				PreConfig: func() { f.MakeDefaultOutOfBand(fakeDefaultContactID) },
				Config:    provider + testAccContactConfig("test", `  firstname = "anonymized"`) + testAccContactConfig("other", ""),
				Check:     resource.TestCheckResourceAttr(testContactOther, "default_contact", "false"),
			},
		},
	})
}

// If another contact takes the default right after the write that made this
// contact the default, the error says so instead of Terraform reporting a
// provider bug.
func TestAccContact_defaultTakenConcurrently(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	config := testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", "  default_contact = true\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { f.StealDefaultAfterNextContactWrite(fakeDefaultContactID) },
				Config:      config,
				ExpectError: wrapped("Another contact became the default contact", "set default_contact = true on one"),
			},
			{
				// The contact is tainted, so it is replaced, and becomes the default.
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(captureResourceID(testContactName, &id), checkFakeDefault(f, &id)),
			},
			{
				PreConfig: func() { f.MakeDefaultOutOfBand(fakeDefaultContactID) },
				Config:    testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", ""),
			},
		},
	})
}

// Moving the default to a new contact while removing the old default works in
// one apply. Terraform runs both at once, and the API refuses to delete the
// old contact until the new one has taken the default.
func TestAccContact_defaultMovesToANewContactInOneApply(t *testing.T) {
	f := newFakeAPI(t)
	var oldID, newID string
	seed := fakeDefaultContactID
	provider := testAccProviderBlock(f.BaseURL())
	makeDefault := "  default_contact = true\n"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: provider + testAccContactConfig("test", makeDefault),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testContactName, &oldID),
					checkFakeDefault(f, &oldID),
				),
			},
			{
				Config: provider + testAccContactConfig("other", makeDefault),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testContactOther, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testContactOther, &newID),
					checkFakeDefault(f, &newID),
					func(*terraform.State) error {
						if slices.Contains(f.ContactIDs(), oldID) {
							return fmt.Errorf("the old default contact %s still exists", oldID)
						}
						return nil
					},
				),
			},
			{
				// Hand the default back to the account's original contact so
				// that the last contact can be destroyed.
				PreConfig: func() { f.MakeDefaultOutOfBand(seed) },
				Config:    provider + testAccContactConfig("other", ""),
			},
		},
	})
}
