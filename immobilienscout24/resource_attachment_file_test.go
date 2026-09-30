package immobilienscout24

// Acceptance tests of how the file attachments handle their local file and
// untitled uploads, against the fake API.

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// An attachment uploaded without a title gets one from ImmobilienScout24: the
// file name without its extension, or "Link" for a link (observed on the
// sandbox, 2026-09-30). It must settle into the state without a diff.
func TestAccAttachment_untitledGetsTheServerTitle(t *testing.T) {
	f := newFakeAPI(t)
	photo := copyTestFile(t, "anonymized.jpg", "living-room.jpg")
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccAttachmentConfig("picture", "test", fileArgument(photo)) +
		testAccAttachmentConfig("link", "test", `  url = "https://www.immobilienscout24.de"`+"\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testPictureName, "title", "living-room"),
					resource.TestCheckResourceAttr(testLinkName, "title", "Link"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// A later update sends the title the attachment has.
				Config: testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
					testAccAttachmentConfig("picture", "test", fileArgument(photo)+"  floorplan = true\n") +
					testAccAttachmentConfig("link", "test", `  url = "https://www.immobilienscout24.de/wohnen/"`+"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testLinkName, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(testPictureName, "title", "living-room"),
					resource.TestCheckResourceAttr(testLinkName, "title", "Link"),
				),
			},
		},
	})
}

// terraform destroy after the configured file was deleted. The destroy starts
// with a refresh that plans the resource too, so reading the file must not
// block it.
func TestAccAttachmentPicture_destroyAfterFileDeleted(t *testing.T) {
	f := newFakeAPI(t)
	photo := copyTestFile(t, "anonymized.jpg", "living-room.jpg")
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccAttachmentConfig("picture", "test", fileArgument(photo))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A plan without the file keeps what was uploaded.
				PreConfig: func() {
					if err := os.Remove(photo); err != nil {
						t.Fatal(err)
					}
				},
				Config:   config,
				PlanOnly: true,
			},
			{
				Config:  config,
				Destroy: true,
			},
		},
	})
}

// rewriteFile is a PreApply plan check with a side effect: it gives the file
// other content after the plan was made and before it is applied.
type rewriteFile struct {
	t    *testing.T
	path string
}

func (r rewriteFile) CheckPlan(_ context.Context, _ plancheck.CheckPlanRequest, _ *plancheck.CheckPlanResponse) {
	content, _ := otherJPEG(r.t)
	if err := os.WriteFile(r.path, content, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// A file that changes between plan and apply stops the apply: Terraform plans
// again during the apply, sees another file_sha256 and reports an
// inconsistent final plan. Nothing is uploaded with the wrong file.
func TestAccAttachmentPicture_fileChangedAfterPlanUploadsNothing(t *testing.T) {
	f := newFakeAPI(t)
	photo := copyTestFile(t, "anonymized.jpg", "living-room.jpg")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{{
			Config: testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
				testAccAttachmentConfig("picture", "test", fileArgument(photo)),
			ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{rewriteFile{t, photo}}},
			ExpectError:      regexp.MustCompile(`inconsistent final plan`),
		}},
	})
	if n := len(f.Requests("POST")); n != 1 {
		t.Errorf("%d POST requests, want 1 (the listing, no upload)", n)
	}
}
