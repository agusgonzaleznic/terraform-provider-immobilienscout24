package immobilienscout24

// Acceptance tests of immobilienscout24_attachment_pdf and
// immobilienscout24_attachment_link, and of all three attachment resources
// together, against the fake API. They need TF_ACC=1 and a Terraform or
// OpenTofu CLI, but no network and no credentials.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccAttachmentPDF_lifecycle(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, id string
	document := copyTestFile(t, "anonymized.pdf", "floor-plan.pdf")
	config := func(extra string) string {
		return testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
			testAccAttachmentConfig("pdf", "test", fileArgument(document)+extra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: config("  title     = \"anonymized\"\n  floorplan = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testPDFName, &id),
					resource.TestCheckResourceAttr(testPDFName, "file_sha256", anonymizedPDFSHA256),
					resource.TestCheckResourceAttr(testPDFName, "content_type", "application/pdf"),
					resource.TestCheckResourceAttr(testPDFName, "floorplan", "true"),
					resource.TestCheckNoResourceAttr(testPDFName, "title_picture"),
					checkFakeUpload(f, &id, anonymizedPDFSHA256, "application/pdf"),
					checkFakeAttachment(f, &id, "externalCheckSum", anonymizedPDFSHA256),
					checkAttachmentWrite(f, "POST", nil, []string{`filename="floor-plan.pdf"`, "Content-Type: application/pdf; name=floor-plan.pdf",
						`xsi:type="common:PDFDocument"`, "</externalCheckSum><floorplan>true</floorplan></common:attachment>"}, []string{"titlePicture"}),
					// A PDF document is in the attachment order, but no title picture.
					func(*terraform.State) error {
						if order, title := f.AttachmentOrder(listingID); !slices.Equal(order, []string{id}) || title != "" {
							return fmt.Errorf("attachment order %v, title picture %q", order, title)
						}
						return nil
					},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: config("  title = \"anonymized, updated\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPDFName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testPDFName, "id", &id),
					resource.TestCheckResourceAttr(testPDFName, "floorplan", "false"),
					checkAttachmentWrite(f, "PUT", &id, []string{"<title>anonymized, updated</title>",
						"<externalCheckSum>" + anonymizedPDFSHA256 + "</externalCheckSum><floorplan>false</floorplan>"}, []string{"titlePicture"}),
					checkFakeAttachment(f, &id, "floorplan", "false"),
				),
			},
			{
				ResourceName:            testPDFName,
				ImportState:             true,
				ImportStateIdFunc:       attachmentImportID(testPDFName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"file", "content_type"},
			},
		},
	})
}

func TestAccAttachmentLink_lifecycle(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, id string
	config := func(url, extra string) string {
		return testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
			testAccAttachmentConfig("link", "test", fmt.Sprintf("  url = %q\n", url)+extra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: config("https://www.immobilienscout24.de", "  title       = \"anonymized\"\n  external_id = \"tf-acc-link\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testLinkName, &id),
					resource.TestCheckResourceAttr(testLinkName, "url", "https://www.immobilienscout24.de"),
					checkFakeAttachment(f, &id, "url", "https://www.immobilienscout24.de"),
					checkFakeAttachment(f, &id, "externalId", "tf-acc-link"),
					// A plain XML body, which the fake also checks by its Content-Type.
					checkAttachmentWrite(f, "POST", nil, []string{`<common:attachment xsi:type="common:Link"`,
						"<title>anonymized</title><externalId>tf-acc-link</externalId><url>https://www.immobilienscout24.de</url>"},
						[]string{"form-data", "floorplan"}),
					// Links are not in the attachment order.
					func(*terraform.State) error {
						if order, _ := f.AttachmentOrder(listingID); len(order) != 0 {
							return fmt.Errorf("attachment order %v, want none", order)
						}
						return nil
					},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: config("https://www.immobilienscout24.de/wohnen/", "  title = \"anonymized\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testLinkName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testLinkName, "id", &id),
					resource.TestCheckNoResourceAttr(testLinkName, "external_id"),
					checkFakeAttachment(f, &id, "url", "https://www.immobilienscout24.de/wohnen/"),
					checkFakeAttachment(f, &id, "externalId", ""),
				),
			},
			{
				ResourceName:      testLinkName,
				ImportState:       true,
				ImportStateIdFunc: attachmentImportID(testLinkName),
				ImportStateVerify: true,
			},
		},
	})
}

// Deleting a listing deletes its attachments, so the plan creates the listing
// and all of them anew.
func TestAccAttachment_listingDeletedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, newListingID string
	ids, newIDs := make([]string, 3), make([]string, 3)
	names := []string{testPictureName, testPDFName, testLinkName}
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccAttachmentConfig("picture", "test", fileArgument(testFile(t, "anonymized.jpg"))) +
		testAccAttachmentConfig("pdf", "test", fileArgument(testFile(t, "anonymized.pdf"))) +
		testAccAttachmentConfig("link", "test", `  url = "https://www.immobilienscout24.de"`)
	capture := func(listing *string, into []string) resource.TestCheckFunc {
		checks := []resource.TestCheckFunc{captureID(listing)}
		for i, name := range names {
			checks = append(checks, captureResourceID(name, &into[i]))
		}
		return resource.ComposeAggregateTestCheckFunc(checks...)
	}
	var creates []plancheck.PlanCheck
	for _, name := range append([]string{testResourceName}, names...) {
		creates = append(creates, plancheck.ExpectResourceAction(name, plancheck.ResourceActionCreate))
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: config, Check: capture(&listingID, ids)},
			{
				PreConfig: func() {
					f.DeleteOutOfBand(listingID)
					for _, id := range ids {
						if slices.Contains(f.AttachmentIDs(), id) {
							t.Fatalf("attachment %s survived its listing", id)
						}
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             creates,
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					capture(&newListingID, newIDs),
					func(*terraform.State) error {
						for i := range ids {
							if newIDs[i] == ids[i] {
								return fmt.Errorf("%s still has the id %s", names[i], ids[i])
							}
						}
						if got := f.AttachmentIDs(); !slices.Equal(got, slices.Sorted(slices.Values(newIDs))) {
							return fmt.Errorf("attachments on the fake API = %v, want %v", got, newIDs)
						}
						return nil
					},
					resource.TestCheckResourceAttrPtr(testLinkName, "real_estate_id", &newListingID),
				),
			},
		},
	})
}

func TestAccAttachment_invalidArguments(t *testing.T) {
	f := newFakeAPI(t)
	dir := t.TempDir()
	bitmap, empty := filepath.Join(dir, "picture.bmp"), filepath.Join(dir, "empty.jpg")
	for name, content := range map[string][]byte{bitmap: readTestdata(t, "anonymized.jpg"), empty: nil} {
		if err := os.WriteFile(name, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	jpg := testFile(t, "anonymized.jpg")
	attachment := func(typ, extra string) string {
		return testAccProviderBlock(f.BaseURL()) + fmt.Sprintf(`
resource "immobilienscout24_attachment_%s" "test" {
  real_estate_id = "315000001"
%s
}
`, typ, extra)
	}
	var steps []resource.TestStep
	for _, tc := range []struct {
		typ, extra string
		want       []string
	}{
		{"picture", fileArgument(jpg) + `  title = "anonymized, anonymized, anonymized"`, []string{"UTF-8 character count must be at most 30"}},
		{"picture", fileArgument(bitmap), []string{"Unknown file type", "Use a file ending in .gif, .jpeg, .jpg, .png, or set content_type"}},
		{"picture", fileArgument(filepath.Join(dir, "missing.jpg")), []string{"Cannot read file", "no such file or directory"}},
		{"picture", fileArgument(empty), []string{"Cannot read file", "is empty"}},
		{"picture", fileArgument(bitmap) + `  content_type = "BMP"`, []string{"Invalid content type", "is not a media type"}},
		{"pdf", fileArgument(jpg), []string{"Unknown file type", "Use a file ending in .pdf, or set content_type"}},
		{"link", `  url = "www.immobilienscout24.de"`, []string{"Invalid URL", "has no http or https scheme"}},
		{"link", `  url = "https://www.immobilienscout24.de"` + "\n  external_id = \"" + strings.Repeat("x", 51) + `"`,
			[]string{"UTF-8 character count must be at most 50"}},
	} {
		steps = append(steps, resource.TestStep{Config: attachment(tc.typ, tc.extra), ExpectError: wrapped(tc.want...)})
	}
	steps = append(steps, resource.TestStep{
		Config:      strings.Replace(attachment("link", `  url = "https://www.immobilienscout24.de"`), `"315000001"`, `"0315000001"`, 1),
		ExpectError: wrapped("real_estate_id must be a whole number in digits, without a leading zero"),
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkAttachmentsDestroyed(f),
		Steps:                    steps,
	})
	if n := len(f.Requests("POST")); n != 0 {
		t.Fatalf("%d POST requests reached the API, want none", n)
	}
}

func TestAccAttachment_realEstateNotFound(t *testing.T) {
	f := newFakeAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkAttachmentsDestroyed(f),
		Steps: []resource.TestStep{{
			Config: testAccProviderBlock(f.BaseURL()) + `
resource "immobilienscout24_attachment_link" "test" {
  real_estate_id = "315000001"
  url            = "https://www.immobilienscout24.de"
}
`,
			ExpectError: wrapped("Real estate not found", "Real estate 315000001 does not exist", "cannot be attached to it"),
		}},
	})
}

func TestAccAttachment_importRejectsInvalidID(t *testing.T) {
	f := newFakeAPI(t)
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccAttachmentConfig("link", "test", `  url = "https://www.immobilienscout24.de"`)
	steps := []resource.TestStep{{Config: config}}
	for _, id := range []string{"904864036", "315000001/ext-tf-acc-link", "0315000001/904864036", "315000001/904864036/1"} {
		steps = append(steps, resource.TestStep{
			Config:        config,
			ResourceName:  testLinkName,
			ImportState:   true,
			ImportStateId: id,
			ExpectError:   regexp.MustCompile(`Invalid import ID`),
		})
	}
	// A picture imported as a link names the resource to import it with.
	steps = append(steps, resource.TestStep{
		PreConfig:     func() { f.AddPictureOutOfBand("315000001", "") },
		Config:        config,
		ResourceName:  testLinkName,
		ImportState:   true,
		ImportStateId: fmt.Sprintf("315000001/%d", fakeAttachmentFirstID+1),
		ExpectError:   wrapped("is a common:Picture, not a common:Link", "import it as immobilienscout24_attachment_picture instead"),
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps:                    steps,
	})
}
