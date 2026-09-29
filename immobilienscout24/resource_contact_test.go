package immobilienscout24

// Acceptance tests of immobilienscout24_contact against the fake API. Like
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
	testContactName  = "immobilienscout24_contact.test"
	testContactOther = "immobilienscout24_contact.other"
)

// testAccContactConfig renders a contact; extra is spliced in verbatim.
func testAccContactConfig(name, extra string) string {
	return fmt.Sprintf(`
resource "immobilienscout24_contact" %q {
  email    = "tf-acc-%s@is24-test.de"
  lastname = "anonymized"
%s
}
`, name, name, extra)
}

// wrapped matches phrases, in this order, in Terraform's output. Terraform
// wraps long lines, so a space in a phrase matches any run of whitespace.
func wrapped(phrases ...string) *regexp.Regexp {
	parts := make([]string, len(phrases))
	for i, p := range phrases {
		parts[i] = strings.Join(strings.Fields(regexp.QuoteMeta(p)), `\s+`)
	}
	return regexp.MustCompile(strings.Join(parts, `[\s\S]*`))
}

// checkFakeContactHas asserts an element of a contact on the fake API; an
// empty want asserts that the contact lacks it.
func checkFakeContactHas(f *fakeAPI, id *string, element, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, ok := f.ContactField(*id, element)
		switch {
		case want == "" && ok:
			return fmt.Errorf("contact %s still has <%s>%s</%s>", *id, element, got, element)
		case want != "" && got != want:
			return fmt.Errorf("contact %s has <%s>%s</%s>, want %q", *id, element, got, element, want)
		}
		return nil
	}
}

// lastContactWrite returns the body of the last request with this method to a contact.
func lastContactWrite(f *fakeAPI, method string, id *string) (string, error) {
	var bodies []string
	for _, r := range f.Requests(method) {
		if r.Path == fakeContactPath || (id != nil && r.Path == fakeContactPath+"/"+*id) {
			bodies = append(bodies, r.Body)
		}
	}
	if len(bodies) == 0 {
		return "", fmt.Errorf("no %s request for the contact reached the API", method)
	}
	return bodies[len(bodies)-1], nil
}

// checkContactWrite asserts that the last write of the contact carried every
// snippet in want and none in unwanted.
func checkContactWrite(f *fakeAPI, method string, id *string, want, unwanted []string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		body, err := lastContactWrite(f, method, id)
		if err != nil {
			return err
		}
		for _, s := range want {
			if !strings.Contains(body, s) {
				return fmt.Errorf("%s body lacks %s:\n%s", method, s, body)
			}
		}
		for _, s := range unwanted {
			if strings.Contains(body, s) {
				return fmt.Errorf("%s body contains %s:\n%s", method, s, body)
			}
		}
		return nil
	}
}

// checkContactsDestroyed asserts that a DELETE reached the API for every
// contact the provider created, except those deleted out of band, that none
// of them is left, and that the account's original default contact is the
// default again.
func checkContactsDestroyed(f *fakeAPI) resource.TestCheckFunc {
	return func(*terraform.State) error {
		deleted := map[string]bool{}
		for _, r := range f.Requests("DELETE") {
			if id, ok := strings.CutPrefix(r.Path, fakeContactPath+"/"); ok {
				deleted[id] = true
			}
		}
		created, outOfBand := f.ContactHistory()
		remaining := f.ContactIDs()
		for _, id := range created {
			if !deleted[id] && !outOfBand[id] {
				return fmt.Errorf("no DELETE request for contact %s reached the API", id)
			}
			if slices.Contains(remaining, id) {
				return fmt.Errorf("contact %s still exists on the fake API after destroy", id)
			}
		}
		if got := f.DefaultContactID(); got != fakeDefaultContactID {
			return fmt.Errorf("the default contact is %s after destroy, want the account's original %s", got, fakeDefaultContactID)
		}
		return nil
	}
}

func TestAccContact_lifecycle(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	provider := testAccProviderBlock(f.BaseURL())
	full := `
  salutation           = "FEMALE"
  firstname            = "anonymized"
  title                = "Dr."
  addition_name        = "anonymized"
  phone_number         = "+49 30 24301999"
  cell_phone_number    = "+49 170 24301999"
  fax_number           = "+49 30 24301998"
  country_code         = "DEU"
  homepage_url         = "https://www.immobilienscout24.de"
  position             = "anonymized"
  secondary_email      = "tf-acc-second@is24-test.de"
  external_id          = "tf-acc-contact"
  show_on_profile_page = true

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }
`
	// Changes the first name, and drops the phone number, the title, the
	// addition to the name, the address and show_on_profile_page.
	updated := `
  salutation        = "FEMALE"
  firstname         = "anonymized, updated"
  cell_phone_number = "+49 170 24301999"
  fax_number        = "+49 30 24301998"
  country_code      = "DEU"
  homepage_url      = "https://www.immobilienscout24.de"
  position          = "anonymized"
  secondary_email   = "tf-acc-second@is24-test.de"
  external_id       = "tf-acc-contact"
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: provider + testAccContactConfig("test", full),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testContactName, &id),
					resource.TestCheckResourceAttrSet(testContactName, "id"),
					resource.TestCheckResourceAttr(testContactName, "phone_number", "+49 30 24301999"),
					resource.TestCheckResourceAttr(testContactName, "address.city", "Berlin"),
					resource.TestCheckResourceAttr(testContactName, "addition_name", "anonymized"),
					checkFakeContactHas(f, &id, "additionName", "anonymized"),
					resource.TestCheckResourceAttr(testContactName, "show_on_profile_page", "true"),
					resource.TestCheckResourceAttr(testContactName, "default_contact", "false"),
					checkFakeContactHas(f, &id, "phoneNumber", "+49 30 24301999"),
					checkContactWrite(f, "POST", nil, []string{"<email>tf-acc-test@is24-test.de</email>",
						"<phoneNumber>+49 30 24301999</phoneNumber>", "<showOnProfilePage>true</showOnProfilePage>"},
						[]string{"defaultContact", "phoneNumberCountryCode"}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// PUT must carry the complete document, and leave out what
				// the configuration no longer sets.
				Config: provider + testAccContactConfig("test", updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testContactName, "id", &id),
					resource.TestCheckResourceAttr(testContactName, "firstname", "anonymized, updated"),
					resource.TestCheckNoResourceAttr(testContactName, "phone_number"),
					resource.TestCheckNoResourceAttr(testContactName, "address"),
					resource.TestCheckNoResourceAttr(testContactName, "addition_name"),
					resource.TestCheckResourceAttr(testContactName, "show_on_profile_page", "false"),
					checkContactWrite(f, "PUT", &id, []string{
						"<email>tf-acc-test@is24-test.de</email>", "<salutation>FEMALE</salutation>",
						"<firstname>anonymized, updated</firstname>", "<lastname>anonymized</lastname>",
						"<faxNumber>+49 30 24301998</faxNumber>", "<cellPhoneNumber>+49 170 24301999</cellPhoneNumber>",
						"<countryCode>DEU</countryCode>", "<homepageUrl>https://www.immobilienscout24.de</homepageUrl>",
						"<position>anonymized</position>", "<secondaryEmail>tf-acc-second@is24-test.de</secondaryEmail>",
						"<externalId>tf-acc-contact</externalId>", "<showOnProfilePage>false</showOnProfilePage>",
					}, []string{"<phoneNumber>", "<title>", "<address>", "defaultContact"}),
					checkFakeContactHas(f, &id, "phoneNumber", ""),
					checkFakeContactHas(f, &id, "phoneNumberSubscriber", ""),
					checkFakeContactHas(f, &id, "title", ""),
					checkFakeContactHas(f, &id, "firstname", "anonymized, updated"),
				),
			},
			{
				ResourceName:      testContactName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The sandbox fills in the salutation and the flags a request leaves out.
func TestAccContact_minimal(t *testing.T) {
	f := newFakeAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{
				Config: testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testContactName, "salutation", "NO_SALUTATION"),
					resource.TestCheckResourceAttr(testContactName, "show_on_profile_page", "false"),
					resource.TestCheckResourceAttr(testContactName, "default_contact", "false"),
					resource.TestCheckNoResourceAttr(testContactName, "country_code"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      testContactName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Read maps what the website changed, the default flag included, and the
// update that follows sends no defaultContact, which keeps the flag.
func TestAccContact_changedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	config := testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", `  firstname = "anonymized"`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureResourceID(testContactName, &id)},
			{
				PreConfig: func() {
					f.SetContactOutOfBand(id, "firstname", "edited on the website")
					f.MakeDefaultOutOfBand(id)
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkFakeContactHas(f, &id, "firstname", "anonymized"),
					resource.TestCheckResourceAttr(testContactName, "default_contact", "true"),
					checkFakeDefault(f, &id),
					checkContactWrite(f, "PUT", &id, nil, []string{"defaultContact"}),
				),
			},
			{
				PreConfig: func() { f.MakeDefaultOutOfBand(fakeDefaultContactID) },
				Config:    config,
				Check:     resource.TestCheckResourceAttr(testContactName, "default_contact", "false"),
			},
		},
	})
}

func TestAccContact_deletedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var first, second string
	config := testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureResourceID(testContactName, &first)},
			{
				PreConfig: func() { f.DeleteContactOutOfBand(first) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionCreate)},
				},
				Check: resource.ComposeTestCheckFunc(
					captureResourceID(testContactName, &second),
					func(*terraform.State) error {
						if second == first {
							return fmt.Errorf("expected a new contact, id is still %s", first)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccContact_invalidArguments(t *testing.T) {
	f := newFakeAPI(t)
	contact := func(extra string) string {
		return testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", extra)
	}
	var steps []resource.TestStep
	for _, tc := range []struct {
		extra string
		want  []string
	}{
		{"  default_contact = false", []string{"default_contact cannot be false", "set default_contact = true on another"}},
		{`  phone_number = "0049 30 24301999"`, []string{"Invalid phone number", "starts with 00"}},
		{`  cell_phone_number = "+49 0170 24301999"`, []string{"Invalid phone number", "must not start with 0"}},
		{`  fax_number = "+4930 24301999"`, []string{"Invalid phone number", "separated by spaces"}},
		{`  secondary_email = "anonymized"`, []string{"must be an email address"}},
		{`  country_code = "de"`, []string{"must be three upper-case letters"}},
		{`  homepage_url = "www.immobilienscout24.de"`, []string{"Invalid URL", "has no http or https scheme"}},
		{`  title = "Professor Doktor Doktor"`, []string{"UTF-8 character count must be at most 15"}},
		{"  address = {}", []string{"Empty address", "Set at least one of street"}},
	} {
		steps = append(steps, resource.TestStep{Config: contact(tc.extra), ExpectError: wrapped(tc.want...)})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps:                    steps,
	})
	if n := len(f.Requests("POST")); n != 0 {
		t.Fatalf("%d POST requests reached the API, want none", n)
	}
}

func TestAccContact_importRejectsInvalidID(t *testing.T) {
	f := newFakeAPI(t)
	config := testAccProviderBlock(f.BaseURL()) + testAccContactConfig("test", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkContactsDestroyed(f),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:        config,
				ResourceName:  testContactName,
				ImportState:   true,
				ImportStateId: "ext-tf-acc-contact",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
			{
				Config:        config,
				ResourceName:  testContactName,
				ImportState:   true,
				ImportStateId: "0" + fakeDefaultContactID,
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}
