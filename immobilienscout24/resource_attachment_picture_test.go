package immobilienscout24

// Acceptance tests of immobilienscout24_attachment_picture against the fake
// API. Like the apartment tests, they need TF_ACC=1 and a Terraform or
// OpenTofu CLI, but no network and no credentials.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	testPictureName      = "immobilienscout24_attachment_picture.test"
	testOtherPictureName = "immobilienscout24_attachment_picture.other"
	testPDFName          = "immobilienscout24_attachment_pdf.test"
	testLinkName         = "immobilienscout24_attachment_link.test"
)

// testAccAttachmentConfig renders an attachment of the test apartment of the
// given type, such as picture; extra is spliced in verbatim.
func testAccAttachmentConfig(typ, name, extra string) string {
	return fmt.Sprintf(`
resource "immobilienscout24_attachment_%s" %q {
  real_estate_id = immobilienscout24_apartment_rent.test.id
%s
}
`, typ, name, extra)
}

// fileArgument is the file argument for path.
func fileArgument(path string) string {
	return fmt.Sprintf("  file = %q\n", path)
}

// testFile returns the absolute path of a file in testdata: Terraform runs in
// a working directory of its own.
func testFile(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// copyTestFile copies a file of testdata to a temporary directory as name.
func copyTestFile(t *testing.T, from, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, readTestdata(t, from), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// otherJPEG is a JPEG with other content than testdata/anonymized.jpg.
func otherJPEG(t *testing.T) (content []byte, sha256Hex string) {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b.Bytes())
	return b.Bytes(), hex.EncodeToString(sum[:])
}

// attachmentImportID is the import id of an attachment in the state.
func attachmentImportID(name string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("%s not in state", name)
		}
		return rs.Primary.Attributes["real_estate_id"] + "/" + rs.Primary.ID, nil
	}
}

// checkFakeAttachment asserts an element of an attachment on the fake API; an
// empty want asserts that the attachment lacks it.
func checkFakeAttachment(f *fakeAPI, id *string, element, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, ok := f.AttachmentField(*id, element)
		switch {
		case want == "" && ok:
			return fmt.Errorf("attachment %s still has <%s>%s</%s>", *id, element, got, element)
		case want != "" && got != want:
			return fmt.Errorf("attachment %s has <%s>%s</%s>, want %q", *id, element, got, element, want)
		}
		return nil
	}
}

// checkFakeUpload asserts which file the fake API received for an attachment.
func checkFakeUpload(f *fakeAPI, id *string, sha256Hex, contentType string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if sum, ct := f.AttachmentUpload(*id); sum != sha256Hex || ct != contentType {
			return fmt.Errorf("attachment %s has a file with SHA-256 %q and type %q, want %q and %q", *id, sum, ct, sha256Hex, contentType)
		}
		return nil
	}
}

// checkFakeTitlePicture asserts the attachment order and title picture of a listing.
func checkFakeTitlePicture(f *fakeAPI, listingID *string, order ...*string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		var want []string
		for _, id := range order {
			want = append(want, *id)
		}
		got, title := f.AttachmentOrder(*listingID)
		if !slices.Equal(got, want) || title != want[0] {
			return fmt.Errorf("attachment order %v with title picture %s, want %v", got, title, want)
		}
		return nil
	}
}

// checkAttachmentWrite asserts that the last request with this method to an
// attachment, or to the collection when id is nil, carried every snippet in
// want and none in unwanted.
func checkAttachmentWrite(f *fakeAPI, method string, id *string, want, unwanted []string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		var bodies []string
		for _, r := range f.Requests(method) {
			if (id == nil && strings.HasSuffix(r.Path, "/attachment")) || (id != nil && strings.HasSuffix(r.Path, "/attachment/"+*id)) {
				bodies = append(bodies, r.Body)
			}
		}
		if len(bodies) == 0 {
			return fmt.Errorf("no %s request for the attachment reached the API", method)
		}
		body := bodies[len(bodies)-1]
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

// checkAttachmentsDestroyed asserts that a DELETE reached the API for every
// attachment the provider created, except those removed out of band, and that
// none is left.
func checkAttachmentsDestroyed(f *fakeAPI) resource.TestCheckFunc {
	return func(*terraform.State) error {
		deleted := map[string]bool{}
		for _, r := range f.Requests("DELETE") {
			if _, id, ok := strings.Cut(r.Path, "/attachment/"); ok {
				deleted[id] = true
			}
		}
		created, outOfBand := f.AttachmentHistory()
		for _, id := range created {
			if !deleted[id] && !outOfBand[id] {
				return fmt.Errorf("no DELETE request for attachment %s reached the API", id)
			}
		}
		if ids := f.AttachmentIDs(); len(ids) > 0 {
			return fmt.Errorf("attachments still exist on the fake API after destroy: %v", ids)
		}
		return nil
	}
}

func testAccAttachmentCheckDestroy(f *fakeAPI) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(checkDestroyed(f), checkAttachmentsDestroyed(f))
}

// differs asserts that a resource got a new id.
func differs(first, second *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if *second == *first {
			return fmt.Errorf("expected a new attachment, id is still %s", *first)
		}
		return nil
	}
}

func TestAccAttachmentPicture_lifecycle(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, first, second string
	photo := copyTestFile(t, "anonymized.jpg", "living-room.jpg")
	replacement, replacementSHA256 := otherJPEG(t)
	config := func(extra string) string {
		return testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
			testAccAttachmentConfig("picture", "test", fileArgument(photo)+extra)
	}
	updated := config("  title     = \"anonymized, updated\"\n  floorplan = true\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{
				Config: config("  title       = \"anonymized\"\n  external_id = \"tf-acc-picture\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(&listingID),
					captureResourceID(testPictureName, &first),
					resource.TestCheckResourceAttrPair(testPictureName, "real_estate_id", testResourceName, "id"),
					resource.TestCheckResourceAttr(testPictureName, "file_sha256", anonymizedJPEGSHA256),
					resource.TestCheckResourceAttr(testPictureName, "content_type", "image/jpeg"),
					resource.TestCheckResourceAttr(testPictureName, "title", "anonymized"),
					resource.TestCheckResourceAttr(testPictureName, "floorplan", "false"),
					// The first picture of a listing is its title picture.
					resource.TestCheckResourceAttr(testPictureName, "title_picture", "true"),
					checkFakeAttachment(f, &first, "externalCheckSum", anonymizedJPEGSHA256),
					checkFakeUpload(f, &first, anonymizedJPEGSHA256, "image/jpeg"),
					checkFakeTitlePicture(f, &listingID, &first),
					checkAttachmentWrite(f, "POST", nil, []string{`name="metadata"; filename="body.xml"`,
						`name="attachment"; filename="living-room.jpg"`, "Content-Type: image/jpeg; name=living-room.jpg",
						"<floorplan>false</floorplan><titlePicture>false</titlePicture>"}, nil),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Metadata changes in place. The PUT sends the checksum again,
				// which it would clear otherwise.
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testPictureName, "id", &first),
					resource.TestCheckResourceAttr(testPictureName, "title", "anonymized, updated"),
					resource.TestCheckNoResourceAttr(testPictureName, "external_id"),
					resource.TestCheckResourceAttr(testPictureName, "floorplan", "true"),
					checkAttachmentWrite(f, "PUT", &first, []string{"<title>anonymized, updated</title>",
						"<externalCheckSum>" + anonymizedJPEGSHA256 + "</externalCheckSum>",
						"<floorplan>true</floorplan><titlePicture>false</titlePicture>"}, []string{"<externalId>"}),
					checkFakeAttachment(f, &first, "externalCheckSum", anonymizedJPEGSHA256),
					checkFakeAttachment(f, &first, "title", "anonymized, updated"),
					checkFakeAttachment(f, &first, "externalId", ""),
				),
			},
			{
				// The API cannot change the file, so new content replaces it.
				PreConfig: func() {
					if err := os.WriteFile(photo, replacement, 0o600); err != nil {
						t.Fatal(err)
					}
				},
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testPictureName, &second),
					differs(&first, &second),
					resource.TestCheckResourceAttr(testPictureName, "file_sha256", replacementSHA256),
					resource.TestCheckResourceAttr(testPictureName, "title", "anonymized, updated"),
					checkFakeUpload(f, &second, replacementSHA256, "image/jpeg"),
					checkFakeAttachment(f, &second, "externalCheckSum", replacementSHA256),
					checkFakeTitlePicture(f, &listingID, &second),
				),
			},
			{
				// A path to the same content changes the picture in place.
				PreConfig: func() {
					if err := os.WriteFile(filepath.Join(filepath.Dir(photo), "moved.jpg"), replacement, 0o600); err != nil {
						t.Fatal(err)
					}
				},
				Config: strings.Replace(updated, "living-room.jpg", "moved.jpg", 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttrPtr(testPictureName, "id", &second),
			},
			{
				ResourceName:      testPictureName,
				ImportState:       true,
				ImportStateIdFunc: attachmentImportID(testPictureName),
				ImportStateVerify: true,
				// ImmobilienScout24 returns neither the file nor its content type.
				ImportStateVerifyIgnore: []string{"file", "content_type"},
			},
		},
	})
}

// Read maps metadata changed by other software, and a checksum that is not
// the file's replaces the picture.
func TestAccAttachmentPicture_changedOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	var first, second string
	config := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "") +
		testAccAttachmentConfig("picture", "test", fileArgument(testFile(t, "anonymized.jpg"))+"  title = \"anonymized\"\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureResourceID(testPictureName, &first)},
			{
				PreConfig: func() { f.SetAttachmentOutOfBand(first, "title", "edited elsewhere") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate)},
				},
				Check: checkFakeAttachment(f, &first, "title", "anonymized"),
			},
			{
				// As another client's PUT without it would.
				PreConfig: func() { f.SetAttachmentOutOfBand(first, "externalCheckSum", "") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureResourceID(testPictureName, &second),
					differs(&first, &second),
					checkFakeAttachment(f, &second, "externalCheckSum", anonymizedJPEGSHA256),
				),
			},
		},
	})
}

// An imported picture whose checksum is the SHA-256 of the configured file
// is kept; one without it is uploaded again.
func TestAccAttachmentPicture_importUploadedElsewhere(t *testing.T) {
	f := newFakeAPI(t)
	var listingID, matching, unknown, replaced string
	file := testFile(t, "anonymized.jpg")
	listing := testAccProviderBlock(f.BaseURL()) + testAccApartmentRentConfig("anonymized", "521.22", "")
	config := listing + testAccAttachmentConfig("picture", "test", fileArgument(file)) +
		testAccAttachmentConfig("picture", "other", fileArgument(file))
	uploads := 0
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccAttachmentCheckDestroy(f),
		Steps: []resource.TestStep{
			{Config: listing, Check: captureID(&listingID)},
			{
				PreConfig: func() {
					matching = f.AddPictureOutOfBand(listingID, anonymizedJPEGSHA256)
					unknown = f.AddPictureOutOfBand(listingID, "")
				},
				Config:             config,
				ResourceName:       testPictureName,
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return listingID + "/" + matching, nil },
				ImportStatePersist: true,
			},
			{
				Config:             config,
				ResourceName:       testOtherPictureName,
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return listingID + "/" + unknown, nil },
				ImportStatePersist: true,
			},
			{
				PreConfig: func() { uploads = len(f.Requests("POST")) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testPictureName, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(testOtherPictureName, plancheck.ResourceActionDestroyBeforeCreate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(testPictureName, "id", &matching),
					resource.TestCheckResourceAttr(testPictureName, "content_type", "image/jpeg"),
					captureResourceID(testOtherPictureName, &replaced),
					differs(&unknown, &replaced),
					func(*terraform.State) error {
						if n := len(f.Requests("POST")) - uploads; n != 1 {
							return fmt.Errorf("%d uploads, want 1", n)
						}
						return nil
					},
				),
			},
		},
	})
}
