package immobilienscout24

// Acceptance tests of what a listing resource does with an answer from the
// API that it cannot use, against the fake API.

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// A NaN where the API should return a number fails the apply with a
// diagnostic instead of crashing the provider, and the listing just created
// stays in the state, tainted: the next apply replaces it instead of leaving it
// orphaned in the account.
func TestAccListing_notANumberFromTheAPI(t *testing.T) {
	f := newFakeAPI(t)
	c := apartmentRentCase
	name, config := c.resourceName(), testAccProviderBlock(f.BaseURL())+c.config("anonymized", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(f),
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { f.ReturnInGet("baseRent", "NaN") },
				Config:      config,
				ExpectError: wrapped("Unexpected apartment for rent from the API", `the API returned "NaN" for baseRent, which is not a number`),
			},
			{
				PreConfig: func() { f.ReturnInGet("baseRent", "") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionReplace)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(s *terraform.State) error {
					created, _ := f.History()
					if len(created) != 2 {
						return fmt.Errorf("listings created: %v, want the tainted one and its replacement", created)
					}
					if _, ok := f.Object(created[0]); ok {
						return fmt.Errorf("the tainted listing %s still exists after the replacement", created[0])
					}
					return resource.TestCheckResourceAttr(name, "id", created[1])(s)
				},
			},
		},
	})
}
