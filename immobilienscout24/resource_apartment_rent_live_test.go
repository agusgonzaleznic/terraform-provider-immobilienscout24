package immobilienscout24

// Live acceptance test against the real ImmobilienScout24 sandbox. It runs
// only with TF_ACC=1, IMMOBILIENSCOUT24_LIVE=1 and all four
// IMMOBILIENSCOUT24_* credential variables set to sandbox credentials, and
// skips otherwise. It makes three write calls (create, update, delete), far below the sandbox limit
// of 200 per minute, and uses the test data the guidelines ask for
// ("anonymized" texts and the ImmobilienScout24 office address).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccLivePreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" || os.Getenv("IMMOBILIENSCOUT24_LIVE") != "1" {
		t.Skip("live sandbox test: set TF_ACC=1 and IMMOBILIENSCOUT24_LIVE=1 to run it")
	}
	for _, c := range credentialEnv {
		if os.Getenv(c.env) == "" {
			t.Skipf("live sandbox test: %s is not set", c.env)
		}
	}
}

func testAccLiveConfig(externalID, title string) string {
	return `
provider "immobilienscout24" {
  environment = "sandbox"
}
` + fmt.Sprintf(`
resource "immobilienscout24_apartment_rent" "test" {
  external_id      = %q
  title            = %q
  show_address     = false
  description_note = "anonymized"

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  base_rent       = 521.22
  living_space    = 72
  number_of_rooms = 3

  courtage = {
    has_courtage = "NO"
  }
}
`, externalID, title)
}

func TestAccApartmentRent_liveSandbox(t *testing.T) {
	testAccLivePreCheck(t)
	externalID := "tf-acc-" + acctest.RandString(10)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccLiveCheckDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccLiveConfig(externalID, "anonymized"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(testResourceName, "id"),
					resource.TestCheckResourceAttr(testResourceName, "external_id", externalID),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: testAccLiveConfig(externalID, "anonymized, updated"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(testResourceName, "title", "anonymized, updated"),
			},
			{
				ResourceName:      testResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccLiveCheckDestroyed asks the sandbox for every destroyed object. The
// documentation does not say whether a deleted object answers 404 or stays
// retrievable in a state such as TO_BE_DELETED; this check expects 404, so a
// failure here answers that open question.
func testAccLiveCheckDestroyed(s *terraform.State) error {
	client := NewClient(sandboxBaseURL,
		os.Getenv("IMMOBILIENSCOUT24_CONSUMER_KEY"), os.Getenv("IMMOBILIENSCOUT24_CONSUMER_SECRET"),
		os.Getenv("IMMOBILIENSCOUT24_ACCESS_TOKEN"), os.Getenv("IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET"),
		"terraform-provider-immobilienscout24/acctest")
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "immobilienscout24_apartment_rent" {
			continue
		}
		_, err := client.GetApartmentRent(context.Background(), rs.Primary.ID)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			return fmt.Errorf("checking that %s is gone: %w", rs.Primary.ID, err)
		default:
			return fmt.Errorf("real estate %s can still be retrieved after destroy", rs.Primary.ID)
		}
	}
	return nil
}
