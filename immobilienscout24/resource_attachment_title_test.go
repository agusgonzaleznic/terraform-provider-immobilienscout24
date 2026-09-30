package immobilienscout24

// Acceptance tests of the title picture rules of
// immobilienscout24_attachment_picture against the fake API. Like the other
// acceptance tests, they need TF_ACC=1 and a Terraform or OpenTofu CLI, but no
// network and no credentials.

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// The first picture becomes the title picture, true moves it, false is
// refused, and deleting the title picture passes it on, without a diff.
func TestAccAttachmentPicture_titlePicture(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, a, b string
	file := testFile(t, "anonymized.jpg")
	listing := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	pictureA := func(extra string) string { return testAccAttachmentConfig("picture", "test", fileArgument(file)+extra) }
	// depends_on uploads A first.
	pictureB := func(extra string) string {
		return testAccAttachmentConfig("picture", "other", fileArgument(file)+
			"  depends_on = [immobilienscout24_attachment_picture.test]\n"+extra)
	}
	makeTitle := "  title_picture = true\n"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: listing + pictureA("") + pictureB(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testPictureName, &a),
					captureResourceID(testOtherPictureName, &b),
					resource.TestCheckResourceAttr(testPictureName, "title_picture", "true"),
					resource.TestCheckResourceAttr(testOtherPictureName, "title_picture", "false"),
					checkFakeTitlePicture(f, &listingID, &a, &b),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// B takes the title picture; A is not written, and its next
				// refresh reads the new flag without a diff.
				Config: listing + pictureA("") + pictureB(makeTitle),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testOtherPictureName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testOtherPictureName, "id", &b),
					resource.TestCheckResourceAttr(testOtherPictureName, "title_picture", "true"),
					checkFakeTitlePicture(f, &listingID, &b, &a),
					checkAttachmentWrite(f, "PUT", &b, []string{"<titlePicture>true</titlePicture>",
						"<externalCheckSum>" + anonymizedJPEGSHA256 + "</externalCheckSum>"}, nil),
				),
			},
			{
				Config:      listing + pictureA("  title_picture = false\n") + pictureB(makeTitle),
				ExpectError: wrapped("title_picture cannot be false", "set title_picture = true on that immobilienscout24_attachment_picture"),
			},
			{
				// Destroying the title picture makes A the title picture again.
				Config: listing + pictureA(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testOtherPictureName, plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkFakeTitlePicture(f, &listingID, &a),
			},
			{
				RefreshState: true,
				Check:        resource.TestCheckResourceAttr(testPictureName, "title_picture", "true"),
			},
		},
	})
}

// Two pictures that set title_picture = true fail the apply that writes both,
// whichever is written first.
func TestAccAttachmentPicture_twoTitlePictures(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, b string
	file := testFile(t, "anonymized.jpg")
	listing := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	makeTitle := "  title_picture = true\n"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: listing + testAccAttachmentConfig("picture", "test", fileArgument(file)+makeTitle) +
					testAccAttachmentConfig("picture", "other", fileArgument(file)+makeTitle),
				ExpectError: wrapped("Two pictures set title_picture = true", "set title_picture = true on one picture per listing only"),
			},
			{
				Config: listing + testAccAttachmentConfig("picture", "test", fileArgument(file)) +
					testAccAttachmentConfig("picture", "other", fileArgument(file)+makeTitle),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testOtherPictureName, &b),
					resource.TestCheckResourceAttr(testOtherPictureName, "title_picture", "true"),
					func(*terraform.State) error {
						if _, title := f.AttachmentOrder(listingID); title != b {
							return fmt.Errorf("the title picture is %s, want %s", title, b)
						}
						return nil
					},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// If another picture takes the title picture right after the write that made
// this one the title picture, the error says so instead of Terraform
// reporting a provider bug, and the next apply takes it back.
func TestAccAttachmentPicture_titleTakenConcurrently(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, a, b string
	file := testFile(t, "anonymized.jpg")
	listing := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	pictures := func(extra string) string {
		return listing + testAccAttachmentConfig("picture", "test", fileArgument(file)) +
			testAccAttachmentConfig("picture", "other", fileArgument(file)+
				"  depends_on = [immobilienscout24_attachment_picture.test]\n"+extra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: pictures(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testPictureName, &a),
					captureResourceID(testOtherPictureName, &b),
				),
			},
			{
				PreConfig: func() { f.StealTitlePictureAfterNextWrite(a) },
				Config:    pictures("  title_picture = true\n"),
				ExpectError: wrapped("Another picture became the title picture",
					"set title_picture = true on one immobilienscout24_attachment_picture per listing only"),
			},
			{
				Config: pictures("  title_picture = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testOtherPictureName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testOtherPictureName, "title_picture", "true"),
					checkFakeTitlePicture(f, &listingID, &b, &a),
				),
			},
		},
	})
}

// Destroying the title picture in the apply that updates another picture can
// make that one the title picture before it is read back. So title_picture is
// known after apply whenever a picture changes, false included, unlike the
// default_contact flag of a contact, and the apply reports no inconsistency.
func TestAccAttachmentPicture_titlePassesOnDuringUpdate(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, a, b string
	file := testFile(t, "anonymized.jpg")
	listing := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: listing + testAccAttachmentConfig("picture", "test", fileArgument(file)) +
					testAccAttachmentConfig("picture", "other", fileArgument(file)+
						"  title_picture = true\n  depends_on    = [immobilienscout24_attachment_picture.test]\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testPictureName, &a),
					captureResourceID(testOtherPictureName, &b),
					checkFakeTitlePicture(f, &listingID, &b, &a),
				),
			},
			{
				// The dependent picture is destroyed before the one it
				// depends on is updated.
				Config: listing + testAccAttachmentConfig("picture", "test", fileArgument(file)+"  title = \"anonymized, updated\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testOtherPictureName, plancheck.ResourceActionDestroy),
						plancheck.ExpectUnknownValue(testPictureName, tfjsonpath.New("title_picture")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testPictureName, "title_picture", "true"),
					checkFakeTitlePicture(f, &listingID, &a),
				),
			},
		},
	})
}
