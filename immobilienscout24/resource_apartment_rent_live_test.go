package immobilienscout24

// Live acceptance test against the real ImmobilienScout24 sandbox. It runs
// only with TF_ACC=1, IMMOBILIENSCOUT24_LIVE=1 and all four
// IMMOBILIENSCOUT24_* credential variables set to sandbox credentials, and
// skips otherwise. It makes fourteen write calls (create the contact, create
// the listing, upload two pictures to it, add a link to it, publish it on
// channel 10000, update it and a picture's title, unpublish it, delete the
// pictures, the link, the listing and the contact), far below the sandbox limit of 200
// per minute, and uses the test data the guidelines ask for ("anonymized"
// texts, an @is24-test.de address, the ImmobilienScout24 office address and
// phone number, and the generated picture testdata/anonymized.jpg). It never
// sets default_contact, which would move the default contact of the sandbox
// account. The last step unpublishes the listing and deletes the picture and
// the link while the listing stays, and asks the sandbox that they are gone:
// deleting the listing would remove them with it and hide a broken delete.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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

// testAccLiveConfig renders the live configuration; title is also the title
// of the picture, which is uploaded from the file picture.
// testAccLiveConfig renders the scenario; full adds the publication and the
// attachments, which the last step removes while the listing stays.
func testAccLiveConfig(externalID, title, picture, kitchen string, full bool) string {
	contact := fmt.Sprintf(`
resource "immobilienscout24_contact" "test" {
  email        = "%s@is24-test.de"
  lastname     = "anonymized"
  phone_number = "+49 30 24301999"

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }
}
`, externalID)
	publication, attachments := "", ""
	if full {
		attachments = fmt.Sprintf(`
resource "immobilienscout24_attachment_picture" "test" {
  real_estate_id = immobilienscout24_apartment_rent.test.id
  file           = %q
  title          = %q
}

resource "immobilienscout24_attachment_link" "test" {
  real_estate_id = immobilienscout24_apartment_rent.test.id
  url            = "https://www.immobilienscout24.de"
  title          = "anonymized"
}

resource "immobilienscout24_attachment_picture" "other" {
  real_estate_id = immobilienscout24_apartment_rent.test.id
  file           = %q
  # Uploaded after the first picture, which therefore is the title picture.
  depends_on = [immobilienscout24_attachment_picture.test]
}
`, picture, title, kitchen)
		publication = `
resource "immobilienscout24_publication" "portal" {
  real_estate_id = immobilienscout24_apartment_rent.test.id
  channel_id     = "10000"
}
`
	}
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
  contact_id       = immobilienscout24_contact.test.id

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
`, externalID, title) + contact + publication + attachments
}

func TestAccApartmentRent_liveSandbox(t *testing.T) {
	testAccLivePreCheck(t)
	externalID := "tf-acc-" + acctest.RandString(10)
	picture := testFile(t, "anonymized.jpg")
	// Untitled, so the provider titles it after the file: the API would show
	// the upload's file name read as Latin-1.
	kitchen := copyTestFile(t, "anonymized.jpg", "Küche 1.jpg")
	var apartmentID, portalID, pictureID, linkID, kitchenID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccLiveCheckDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccLiveConfig(externalID, "anonymized", picture, kitchen, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testResourceName, &apartmentID),
					captureResourceID(testPortalName, &portalID),
					captureResourceID(testPictureName, &pictureID),
					captureResourceID(testLinkName, &linkID),
					captureResourceID(testOtherPictureName, &kitchenID),
					testAccLiveCheckAttachmentTitle(&apartmentID, &kitchenID, "Küche 1"),
					resource.TestCheckResourceAttrSet(testResourceName, "id"),
					resource.TestCheckResourceAttr(testResourceName, "external_id", externalID),
					resource.TestCheckResourceAttrPair(testResourceName, "contact_id", testContactName, "id"),
					testAccLiveCheckListingContact(&apartmentID),
					resource.TestCheckResourceAttr(testContactName, "default_contact", "false"),
					resource.TestCheckResourceAttrPair(testPortalName, "real_estate_id", testResourceName, "id"),
					resource.TestCheckResourceAttr(testPortalName, "channel_id", "10000"),
					resource.TestCheckResourceAttrPair(testPictureName, "real_estate_id", testResourceName, "id"),
					resource.TestCheckResourceAttr(testPictureName, "file_sha256", anonymizedJPEGSHA256),
					// The only picture of the listing is its title picture.
					resource.TestCheckResourceAttr(testPictureName, "title_picture", "true"),
					testAccLiveCheckPicture("anonymized"),
					resource.TestCheckResourceAttrPair(testLinkName, "real_estate_id", testResourceName, "id"),
					resource.TestCheckResourceAttr(testLinkName, "url", "https://www.immobilienscout24.de"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// On the sandbox, a full PUT kept the listing published. The
				// picture's title changes in place, and its checksum stays.
				Config: testAccLiveConfig(externalID, "anonymized, updated", picture, kitchen, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(testContactName, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testLinkName, plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				// The update sends the contact, so the listing keeps it; the
				// sandbox resets a listing to the default contact on a PUT
				// without one.
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testResourceName, "title", "anonymized, updated"),
					resource.TestCheckResourceAttrPair(testResourceName, "contact_id", testContactName, "id"),
					testAccLiveCheckListingContact(&apartmentID),
					testAccLiveCheckPicture("anonymized, updated"),
				),
			},
			{
				ResourceName:      testResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      testPortalName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      testContactName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      testPictureName,
				ImportState:       true,
				ImportStateIdFunc: attachmentImportID(testPictureName),
				ImportStateVerify: true,
				// ImmobilienScout24 returns neither the file nor its content type.
				ImportStateVerifyIgnore: []string{"file", "content_type"},
			},
			{
				ResourceName:      testLinkName,
				ImportState:       true,
				ImportStateIdFunc: attachmentImportID(testLinkName),
				ImportStateVerify: true,
			},
			{
				// Unpublish and delete the attachments while the listing stays,
				// so the sandbox itself shows that each Delete worked; deleting
				// the listing would remove them with it and hide a broken one.
				Config: testAccLiveConfig(externalID, "anonymized, updated", picture, kitchen, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testPortalName, plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testLinkName, plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testOtherPictureName, plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testResourceName, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccLiveCheckUnpublished(&apartmentID, &portalID),
					testAccLiveCheckAttachmentsGone(&apartmentID, &pictureID, &linkID, &kitchenID),
				),
			},
		},
	})
}

// testAccLiveCheckListingContact asks the sandbox itself which contact the
// listing has, and compares it with the contact resource in the state.
func testAccLiveCheckListingContact(apartmentID *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[testContactName]
		if !ok {
			return fmt.Errorf("%s not in state", testContactName)
		}
		doc, err := testAccLiveClient().GetApartmentRent(context.Background(), *apartmentID)
		if err != nil {
			return fmt.Errorf("reading listing %s: %w", *apartmentID, err)
		}
		if doc.Contact == nil || strings.TrimSpace(doc.Contact.ID) != rs.Primary.ID {
			return fmt.Errorf("listing %s has contact %+v on the sandbox, want %s", *apartmentID, doc.Contact, rs.Primary.ID)
		}
		return nil
	}
}

// testAccLiveCheckPicture asks the sandbox for the picture in the state: it
// has the title, still carries the SHA-256 of the file as its checksum, and
// is the listing's title picture.
func testAccLiveCheckPicture(title string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[testPictureName]
		if !ok {
			return fmt.Errorf("%s not in state", testPictureName)
		}
		doc, err := testAccLiveClient().GetAttachment(context.Background(), rs.Primary.Attributes["real_estate_id"], rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("reading picture %s: %w", rs.Primary.ID, err)
		}
		if doc.Type != attachmentPicture || doc.Title != title || doc.ExternalCheckSum != anonymizedJPEGSHA256 ||
			doc.TitlePicture == nil || !*doc.TitlePicture {
			return fmt.Errorf("picture %s on the sandbox: %+v, want title %q and checksum %s", rs.Primary.ID, doc, title, anonymizedJPEGSHA256)
		}
		return nil
	}
}

// captureResourceID stores the id of a resource in the current state in *id.
func captureResourceID(name string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		*id = rs.Primary.ID
		return nil
	}
}

// testAccLiveCheckUnpublished asks the sandbox that the publication is gone and
// the listing is still there.
func testAccLiveCheckUnpublished(apartmentID, portalID *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		client := testAccLiveClient()
		if _, err := client.GetPublication(context.Background(), *portalID); !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("publication %s should be gone after unpublishing, got: %v", *portalID, err)
		}
		if _, err := client.GetApartmentRent(context.Background(), *apartmentID); err != nil {
			return fmt.Errorf("listing %s should still exist after unpublishing: %w", *apartmentID, err)
		}
		return nil
	}
}

// testAccLiveCheckAttachmentTitle asks the sandbox for the title of an
// attachment.
func testAccLiveCheckAttachmentTitle(apartmentID, id *string, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		doc, err := testAccLiveClient().GetAttachment(context.Background(), *apartmentID, *id)
		if err != nil {
			return fmt.Errorf("reading attachment %s: %w", *id, err)
		}
		if doc.Title != want {
			return fmt.Errorf("attachment %s has title %q on the sandbox, want %q", *id, doc.Title, want)
		}
		return nil
	}
}

// testAccLiveCheckAttachmentsGone asks the sandbox that the attachments are
// gone while their listing still exists.
func testAccLiveCheckAttachmentsGone(apartmentID *string, ids ...*string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		client := testAccLiveClient()
		for _, id := range ids {
			if _, err := client.GetAttachment(context.Background(), *apartmentID, *id); !errors.Is(err, ErrNotFound) {
				return fmt.Errorf("attachment %s should be gone after deleting it, got: %v", *id, err)
			}
		}
		return nil
	}
}

func testAccLiveClient() *Client {
	return NewClient(sandboxBaseURL,
		os.Getenv("IMMOBILIENSCOUT24_CONSUMER_KEY"), os.Getenv("IMMOBILIENSCOUT24_CONSUMER_SECRET"),
		os.Getenv("IMMOBILIENSCOUT24_ACCESS_TOKEN"), os.Getenv("IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET"),
		"terraform-provider-immobilienscout24/acctest")
}

// testAccLiveCheckDestroyed asks the sandbox for every destroyed object. The
// documentation does not say whether a deleted object answers 404 or stays
// retrievable in a state such as TO_BE_DELETED; this check expects 404, so a
// failure here answers that open question. An unpublished publication
// answered 404 on the sandbox (observed 2026-09-29).
func testAccLiveCheckDestroyed(s *terraform.State) error {
	client := testAccLiveClient()
	for _, rs := range s.RootModule().Resources {
		var err error
		switch rs.Type {
		case "immobilienscout24_apartment_rent":
			_, err = client.GetApartmentRent(context.Background(), rs.Primary.ID)
		case "immobilienscout24_publication":
			_, err = client.GetPublication(context.Background(), rs.Primary.ID)
		case "immobilienscout24_contact":
			_, err = client.GetContact(context.Background(), rs.Primary.ID)
		case "immobilienscout24_attachment_picture", "immobilienscout24_attachment_pdf", "immobilienscout24_attachment_link":
			_, err = client.GetAttachment(context.Background(), rs.Primary.Attributes["real_estate_id"], rs.Primary.ID)
		default:
			continue
		}
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
