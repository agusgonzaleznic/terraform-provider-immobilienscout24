package immobilienscout24

// Unit tests that hold the attachments of the fake API, on which the
// acceptance tests rely, to what the live sandbox did on 2026-09-30, and a
// test of Delete against it.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// fakeAttachmentClient is a client of a new fake API with one listing.
type fakeAttachmentClient struct {
	t       *testing.T
	f       *fakeAPI
	c       *Client
	listing string
}

func newFakeAttachmentClient(t *testing.T) *fakeAttachmentClient {
	t.Helper()
	f := newFakeAPI(t)
	c := fakeClient(f)
	listing, err := c.CreateApartmentRent(context.Background(), fullModel().toDocument())
	if err != nil {
		t.Fatal(err)
	}
	return &fakeAttachmentClient{t: t, f: f, c: c, listing: listing}
}

// upload sends metadata and a file as the provider does, and returns the new id.
func (a *fakeAttachmentClient) upload(metadata, contentType string, content []byte) (string, error) {
	body, mediaType, err := multipartBody([]byte(metadata), attachmentFile{Name: "anonymized", ContentType: contentType, Content: content})
	if err != nil {
		a.t.Fatal(err)
	}
	return a.send(http.MethodPost, attachmentsPath(a.listing), mediaType, body)
}

// send sends a request and returns the id of a created attachment.
func (a *fakeAttachmentClient) send(method, path, contentType string, body []byte) (string, error) {
	resp, err := a.c.send(context.Background(), a.c.httpClient, method, path, contentType, body)
	if err != nil || method != http.MethodPost {
		return "", err
	}
	return createdAttachmentID(resp)
}

func (a *fakeAttachmentClient) put(id, metadata string) error {
	_, err := a.send(http.MethodPut, attachmentPath(a.listing, id), mediaTypeXML, []byte(metadata))
	return err
}

func (a *fakeAttachmentClient) get(id string) (*attachmentDocument, error) {
	return a.c.GetAttachment(context.Background(), a.listing, id)
}

// order returns the attachment order as GET attachmentsorder answers it.
func (a *fakeAttachmentClient) order() []string {
	a.t.Helper()
	resp, err := a.c.do(context.Background(), http.MethodGet, attachmentsPath(a.listing)+"/attachmentsorder", nil)
	if err != nil {
		a.t.Fatal(err)
	}
	var ids []string
	for _, m := range regexp.MustCompile(`<attachmentId>(\d+)</attachmentId>`).FindAllStringSubmatch(string(resp.body), -1) {
		ids = append(ids, m[1])
	}
	return ids
}

func (a *fakeAttachmentClient) titlePicture() string {
	_, title := a.f.AttachmentOrder(a.listing)
	return title
}

// picture is observedPictureMetadata with another external id and title flag.
func picture(externalID string, titlePicture bool) string {
	s := strings.Replace(observedPictureMetadata, "tf-att-1", externalID, 1)
	if titlePicture {
		s = strings.Replace(s, "<titlePicture>false", "<titlePicture>true", 1)
	}
	return s
}

func wantAPIError(t *testing.T, err error, status int, code, text string) {
	t.Helper()
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != status || len(apiErr.Messages) != 1 ||
		apiErr.Messages[0].Code != code || (text != "" && apiErr.Messages[0].Text != text) {
		t.Fatalf("got %v, want HTTP %d %s %q", err, status, code, text)
	}
}

// The three metadata documents the sandbox refused get its answers.
func TestFakeAttachmentSchemaErrorsAreTheObservedOnes(t *testing.T) {
	a := newFakeAttachmentClient(t)
	jpeg, pdf := readTestdata(t, "anonymized.jpg"), readTestdata(t, "anonymized.pdf")
	const failed = "ERROR_COMMON_SCHEMA_VALIDATION_FAILED"

	_, err := a.upload(observedPictureWithoutTitlePicture, "image/jpeg", jpeg)
	wantAPIError(t, err, http.StatusPreconditionFailed, failed, "The request is not schema valid. [MESSAGE: cvc-complex-type.2.4.b: "+
		"The content of element 'common:attachment' is not complete. One of '{titlePicture}' is expected.]")
	// That answer is the sandbox's, byte for byte.
	body, mediaType, err := multipartBody([]byte(observedPictureWithoutTitlePicture),
		attachmentFile{Name: "anonymized.jpg", ContentType: "image/jpeg", Content: jpeg})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, a.f.BaseURL()+attachmentsPath(a.listing), bytes.NewReader(body))
	req.Header.Set("Content-Type", mediaType)
	req.Header.Set("Accept", mediaTypeXML)
	resp, err := a.c.httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(raw) != observedSchemaErrorBody {
		t.Errorf("412 body =\n%s\nwant\n%s", raw, observedSchemaErrorBody)
	}

	id, err := a.upload(observedPictureMetadata, "image/jpeg", jpeg)
	if err != nil {
		t.Fatal(err)
	}
	wantAPIError(t, a.put(id, observedPictureWithoutFloorplan), http.StatusPreconditionFailed, failed,
		"The request is not schema valid. [MESSAGE: cvc-complex-type.2.4.a: Invalid content was found starting with element "+
			"'titlePicture'. One of '{externalCheckSum, floorplan}' is expected.]")
	pdfWithoutFloorplan := strings.Replace(strings.Replace(observedPDFMetadata, "<floorplan>false</floorplan>", "", 1),
		"<externalCheckSum>794abaa4f6f06fc519895c22944a0ab43ad02b4f</externalCheckSum>", "", 1)
	_, err = a.upload(pdfWithoutFloorplan, "application/pdf", pdf)
	wantAPIError(t, err, http.StatusPreconditionFailed, failed, "The request is not schema valid. [MESSAGE: cvc-complex-type.2.4.b: "+
		"The content of element 'common:attachment' is not complete. One of '{externalCheckSum, url, floorplan}' is expected.]")
}

// An upload has a part named metadata and one named attachment, in either
// order (observed), with the documented headers, and nothing else.
func TestFakeAttachmentUploadParts(t *testing.T) {
	a := newFakeAttachmentClient(t)
	jpeg := readTestdata(t, "anonymized.jpg")
	type part struct {
		disposition, contentType, encoding string
		content                            []byte
	}
	metadata := part{`form-data; name="metadata"; filename="body.xml"`, "application/xml; name=body.xml", "binary",
		[]byte(picture("tf-att-1", false))}
	file := part{`form-data; name="attachment"; filename="anonymized.jpg"`, "image/jpeg; name=anonymized.jpg", "binary", jpeg}
	upload := func(parts ...part) error {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for _, p := range parts {
			h := textproto.MIMEHeader{"Content-Disposition": {p.disposition}, "Content-Type": {p.contentType}}
			if p.encoding != "" {
				h.Set("Content-Transfer-Encoding", p.encoding)
			}
			pw, _ := w.CreatePart(h)
			_, _ = pw.Write(p.content)
		}
		_ = w.Close()
		_, err := a.send(http.MethodPost, attachmentsPath(a.listing), w.FormDataContentType(), b.Bytes())
		return err
	}
	for name, parts := range map[string][]part{"metadata first": {metadata, file}, "attachment first": {file, metadata}} {
		if err := upload(parts...); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	stored := len(a.f.AttachmentIDs())
	renamed := file
	renamed.disposition = strings.Replace(file.disposition, `name="attachment"`, `name="file"`, 1)
	noFileName := file
	noFileName.disposition = `form-data; name="attachment"`
	asPDF := file
	asPDF.contentType = "application/pdf"
	noEncoding := file
	noEncoding.encoding = ""
	// Each refusal must be for its own reason, not for another one.
	for name, tc := range map[string]struct {
		parts []part
		want  string
	}{
		"a part named file":       {[]part{metadata, renamed}, `unexpected part "file"`},
		"no metadata":             {[]part{file}, "needs a part named metadata and one named attachment"},
		"no file":                 {[]part{metadata}, "needs a part named metadata and one named attachment"},
		"two metadata parts":      {[]part{metadata, metadata, file}, `unexpected part "metadata"`},
		"a third part":            {[]part{metadata, file, renamed}, `unexpected part "file"`},
		"no filename":             {[]part{metadata, noFileName}, "the attachment part has no filename"},
		"a picture as a PDF file": {[]part{metadata, asPDF}, `cannot be a file of type "application/pdf"`},
		"no transfer encoding":    {[]part{metadata, noEncoding}, "lacks Content-Transfer-Encoding: binary"},
	} {
		if err := upload(tc.parts...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
		}
	}
	for name, tc := range map[string]struct {
		send func() (string, error)
		want string
	}{
		"a picture as plain XML": {func() (string, error) {
			return a.send(http.MethodPost, attachmentsPath(a.listing), mediaTypeXML, []byte(picture("tf-att-1", false)))
		}, "is uploaded as multipart/form-data"},
		"a link as an upload": {func() (string, error) { return a.upload(observedLinkMetadata, "image/jpeg", jpeg) },
			"takes a plain XML body"},
		"plain text": {func() (string, error) {
			return a.send(http.MethodPost, attachmentsPath(a.listing), "text/plain", []byte("anonymized"))
		}, "ERROR_COMMON_MEDIA_TYPE_UNSUPPORTED"},
	} {
		if _, err := tc.send(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
		}
	}
	if got := len(a.f.AttachmentIDs()); got != stored {
		t.Fatalf("%d attachments after the refused requests, want %d", got, stored)
	}
}

// Facts 11 to 17: the title picture, the attachment order and DELETE.
func TestFakeAttachmentTitlePictureAndOrder(t *testing.T) {
	a := newFakeAttachmentClient(t)
	jpeg, pdf := readTestdata(t, "anonymized.jpg"), readTestdata(t, "anonymized.pdf")
	upload := func(metadata, contentType string, content []byte) string {
		t.Helper()
		id, err := a.upload(metadata, contentType, content)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	isTitle := func(id string) bool {
		t.Helper()
		doc, err := a.get(id)
		if err != nil {
			t.Fatal(err)
		}
		return *doc.TitlePicture
	}
	p1 := upload(picture("tf-att-1", false), "image/jpeg", jpeg)
	p2 := upload(picture("tf-att-2", false), "image/jpeg", jpeg)
	d := upload(observedPDFMetadata, "application/pdf", pdf)
	link, err := a.send(http.MethodPost, attachmentsPath(a.listing), mediaTypeXML, []byte(observedLinkMetadata))
	if err != nil {
		t.Fatal(err)
	}
	if !isTitle(p1) || isTitle(p2) || !slices.Equal(a.order(), []string{p1, p2, d}) {
		t.Fatalf("the first picture must be the title picture although it said false; order %v", a.order())
	}
	if doc, err := a.get(d); err != nil || doc.TitlePicture != nil || !strings.HasPrefix(doc.URL, "https://d12ts8pcffi7qk.cloudfront.net/") {
		t.Fatalf("PDF document: %+v, %v", doc, err)
	}
	if doc, err := a.get(link); err != nil || doc.Type != attachmentLink || doc.URL != "https://www.immobilienscout24.de" {
		t.Fatalf("link: %+v, %v", doc, err)
	}

	if err := a.put(p2, picture("tf-att-2", true)); err != nil {
		t.Fatal(err)
	}
	if !isTitle(p2) || isTitle(p1) || !slices.Equal(a.order(), []string{p2, p1, d}) {
		t.Fatalf("a PUT with true must move the title picture to the front; order %v", a.order())
	}
	for _, id := range []string{p2, p1} {
		if err := a.put(id, picture("tf-att-x", false)); err != nil {
			t.Fatal(err)
		}
	}
	if a.titlePicture() != p2 {
		t.Fatal("a PUT with false must change nothing")
	}

	ctx := context.Background()
	if err := a.c.DeleteAttachment(ctx, a.listing, p2); err != nil {
		t.Fatal(err)
	}
	if a.titlePicture() != p1 || !slices.Equal(a.order(), []string{p1, d}) {
		t.Fatalf("deleting the title picture must make the next picture the title picture; order %v", a.order())
	}
	if err := a.c.DeleteAttachment(ctx, a.listing, p2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DELETE = %v, want ErrNotFound", err)
	}
	if _, err := a.get(p2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GET of a deleted attachment = %v, want ErrNotFound", err)
	}
	// Simulated from the developer FAQ: an upload with true takes the title.
	p3 := upload(picture("tf-att-3", true), "image/jpeg", jpeg)
	if a.titlePicture() != p3 || !slices.Equal(a.order(), []string{p3, p1, d}) {
		t.Fatalf("an upload with true must take the title picture; order %v", a.order())
	}
}

// Facts 5, 8, 9 and 18: the checksum is kept as sent, PUT replaces the
// metadata, ?externalId= answers a list, and deleting the listing deletes
// the attachments.
func TestFakeAttachmentMetadataQueriesAndListingDelete(t *testing.T) {
	a := newFakeAttachmentClient(t)
	jpeg := readTestdata(t, "anonymized.jpg")
	long := strings.Replace(picture("tf-att-1", false), "a2dabab4054e1cbc30c1bace49b073eed8b8a55f", strings.Repeat("a2dab", 25)+"abc", 1)
	id, err := a.upload(long, "image/jpeg", jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if doc, _ := a.get(id); doc.ExternalCheckSum != strings.Repeat("a2dab", 25)+"abc" {
		t.Fatalf("a 128-character checksum came back as %q", doc.ExternalCheckSum)
	}
	if sum, ct := a.f.AttachmentUpload(id); sum != anonymizedJPEGSHA256 || ct != "image/jpeg" {
		t.Fatalf("the fake stored a file with SHA-256 %s and type %s", sum, ct)
	}
	withoutChecksum := strings.Replace(strings.Replace(long, "<floorplan>false", "<floorplan>true", 1),
		"<externalCheckSum>"+strings.Repeat("a2dab", 25)+"abc</externalCheckSum>", "", 1)
	if err := a.put(id, withoutChecksum); err != nil {
		t.Fatal(err)
	}
	if doc, _ := a.get(id); doc.ExternalCheckSum != "" || !*doc.Floorplan {
		t.Fatalf("a PUT without the checksum must clear it and set floorplan: %+v", doc)
	}
	link, err := a.send(http.MethodPost, attachmentsPath(a.listing), mediaTypeXML, []byte(observedLinkMetadata))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.put(link, strings.Replace(observedLinkMetadata, "immobilienscout24.de</url>", "immobilienscout24.de/wohnen/</url>", 1)); err != nil {
		t.Fatal(err)
	}
	if doc, _ := a.get(link); doc.URL != "https://www.immobilienscout24.de/wohnen/" {
		t.Fatalf("the url of a link must change in place, got %q", doc.URL)
	}

	query := func(externalID string) string {
		resp, err := a.c.do(context.Background(), http.MethodGet, attachmentsPath(a.listing)+"?externalId="+externalID, nil)
		if err != nil {
			t.Fatal(err)
		}
		return string(resp.body)
	}
	if got := query("tf-att-1"); strings.Count(got, "<attachment ") != 1 || !strings.Contains(got, `id="`+id+`"`) {
		t.Fatalf("?externalId=tf-att-1 answered\n%s", got)
	}
	if got := query("unknown"); !strings.Contains(got, "<common:attachments ") || strings.Contains(got, "<attachment ") {
		t.Fatalf("?externalId=unknown answered\n%s", got)
	}

	if err := a.c.DeleteRealEstate(context.Background(), a.listing); err != nil {
		t.Fatal(err)
	}
	for _, i := range []string{id, link} {
		if _, err := a.get(i); !errors.Is(err, ErrNotFound) {
			t.Fatalf("attachment %s after its listing was deleted: %v, want ErrNotFound", i, err)
		}
	}
	if ids := a.f.AttachmentIDs(); len(ids) != 0 {
		t.Fatalf("attachments left: %v", ids)
	}
}

// The fake lists two pictures uploaded with the probe's metadata exactly as
// the sandbox did, apart from the values that differ on every upload.
func TestFakeAttachmentBodiesHaveTheObservedShape(t *testing.T) {
	a := newFakeAttachmentClient(t)
	jpeg := readTestdata(t, "anonymized.jpg")
	for _, metadata := range []string{observedPictureMetadata, picture("tf-att-2", false)} {
		if _, err := a.upload(metadata, "image/jpeg", jpeg); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := a.c.do(context.Background(), http.MethodGet, attachmentsPath(a.listing), nil)
	if err != nil {
		t.Fatal(err)
	}
	normalise := func(s string) string {
		s = regexp.MustCompile(`modification="[^"]*"`).ReplaceAllString(s, `modification=""`)
		s = regexp.MustCompile(`listings-test/[0-9a-f-]{36}-`).ReplaceAllString(s, "listings-test/UUID-")
		return strings.NewReplacer("https://rest.sandbox-immobilienscout24.de/restapi/api", a.f.BaseURL(),
			"/realestate/325477914/", "/realestate/"+a.listing+"/").Replace(s)
	}
	if got, want := normalise(string(resp.body)), normalise(observedAttachmentListBody); got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
}

// Terraform refreshes before it deletes, so the acceptance tests never make
// Delete meet an attachment that is already gone. Delete must count that as
// done, and fail on any other error.
func TestAttachmentDeleteHandlesGoneAttachments(t *testing.T) {
	ctx := context.Background()
	a := newFakeAttachmentClient(t)
	jpeg := readTestdata(t, "anonymized.jpg")
	for _, r := range []resource.ResourceWithConfigure{&fileAttachmentResource{kind: pictureKind}, &fileAttachmentResource{kind: pdfKind},
		&linkResource{}} {
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "immobilienscout24"}, &meta)
		var schema resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &schema)
		r.Configure(ctx, resource.ConfigureRequest{ProviderData: a.c}, &resource.ConfigureResponse{})
		id, err := a.upload(picture("tf-att-delete", false), "image/jpeg", jpeg)
		if err != nil {
			t.Fatal(err)
		}
		deleteIt := func() resource.DeleteResponse {
			t.Helper()
			state := tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(ctx), nil)}
			for name, v := range map[string]string{"id": id, "real_estate_id": a.listing} {
				if diags := state.SetAttribute(ctx, path.Root(name), types.StringValue(v)); diags.HasError() {
					t.Fatal(diags)
				}
			}
			var resp resource.DeleteResponse
			r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
			return resp
		}
		before := len(a.f.Requests(http.MethodDelete))
		for i := range 2 {
			if resp := deleteIt(); resp.Diagnostics.HasError() {
				t.Fatalf("%s: delete %d: %v", meta.TypeName, i+1, resp.Diagnostics)
			}
		}
		if n := len(a.f.Requests(http.MethodDelete)) - before; n != 2 {
			t.Fatalf("%s: %d DELETE requests reached the API, want 2", meta.TypeName, n)
		}
		r.Configure(ctx, resource.ConfigureRequest{
			ProviderData: stubServer(t, http.StatusInternalServerError, nil, "Internal Server Error")}, &resource.ConfigureResponse{})
		if resp := deleteIt(); !resp.Diagnostics.HasError() || !strings.HasPrefix(resp.Diagnostics.Errors()[0].Summary(), "Error deleting") {
			t.Fatalf("%s: a 500 must fail the delete, got %v", meta.TypeName, resp.Diagnostics)
		}
	}
}

func TestClaimTitlePicture(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "ck", "cs", "at", "ats", "test")
	if !c.claimTitlePicture("1") || c.claimTitlePicture("1") || !c.claimTitlePicture("2") {
		t.Fatal("a listing's title picture can be claimed once, independently of other listings")
	}
	c.releaseTitlePicture("1")
	if !c.claimTitlePicture("1") {
		t.Fatal("a released claim can be made again")
	}
}
