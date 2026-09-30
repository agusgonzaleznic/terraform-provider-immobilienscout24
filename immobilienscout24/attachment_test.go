package immobilienscout24

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Requests of the sandbox probe and responses of the live sandbox, 2026-09-30,
// verbatim. The files were testdata/anonymized.jpg and testdata/anonymized.pdf.
const (
	anonymizedJPEGSHA256 = "a2dabab4054e1cbc30c1bace49b073eed8b8a55f5a889922cffb663f3e2f01e9"
	anonymizedPDFSHA256  = "794abaa4f6f06fc519895c22944a0ab43ad02b4fb32bdefa1952ce81613cb47b"

	observedAttachmentLocation    = "https://rest.sandbox-immobilienscout24.de/restapi/api/offer/v1.0/user/me/realestate/325477914/attachment/904864036"
	observedAttachmentCreatedBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages ` + observedAttachmentNamespaces + `>
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [attachment] with id [904864036] has been created.</message>
        <id>904864036</id>
    </message>
</common:messages>
`
	// The metadata of the first picture, a PDF document and a link.
	observedPictureMetadata = `<common:attachment xsi:type="common:Picture" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <title>anonymized</title>
    <externalId>tf-att-1</externalId>
    <externalCheckSum>a2dabab4054e1cbc30c1bace49b073eed8b8a55f</externalCheckSum>
    <floorplan>false</floorplan>
    <titlePicture>false</titlePicture>
</common:attachment>
`
	observedPDFMetadata = `<common:attachment xsi:type="common:PDFDocument" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <title>anonymized</title>
    <externalId>tf-att-pdf</externalId>
    <externalCheckSum>794abaa4f6f06fc519895c22944a0ab43ad02b4f</externalCheckSum>
    <floorplan>false</floorplan>
</common:attachment>
`
	observedLinkMetadata = `<common:attachment xsi:type="common:Link" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <title>anonymized</title>
    <externalId>tf-att-link</externalId>
    <url>https://www.immobilienscout24.de</url>
</common:attachment>
`
	// Two picture metadata documents the sandbox refused with 412, as a PUT
	// and as an upload, and its answer to the second.
	observedPictureWithoutFloorplan = `<common:attachment xsi:type="common:Picture" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <title>anonymized</title>
    <externalId>tf-att-2</externalId>
    <titlePicture>true</titlePicture>
</common:attachment>
`
	observedPictureWithoutTitlePicture = `<common:attachment xsi:type="common:Picture" xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <title>anonymized</title>
    <externalCheckSum>a2dabab4054e1cbc30c1bace49b073eed8b8a55f5a889922cffb663f3e2f01e9</externalCheckSum>
    <floorplan>false</floorplan>
</common:attachment>
`
	observedSchemaErrorBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages ` + observedAttachmentNamespaces + `>
    <message>
        <messageCode>ERROR_COMMON_SCHEMA_VALIDATION_FAILED</messageCode>
        <message>The request is not schema valid. [MESSAGE: cvc-complex-type.2.4.b: The content of element 'common:attachment' is not complete. One of '{titlePicture}' is expected.]</message>
    </message>
</common:messages>
`
	// GET .../attachment after two picture uploads.
	observedAttachmentListBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:attachments ` + observedAttachmentNamespaces + `>
    <attachment xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="common:Picture" xlink:href="https://rest.sandbox-immobilienscout24.de/restapi/api/offer/v1.0/user/me/realestate/325477914/attachment/904864036" id="904864036" modification="2026-09-29T23:13:43.296Z">
        <title>anonymized</title>
        <externalId>tf-att-1</externalId>
        <externalCheckSum>a2dabab4054e1cbc30c1bace49b073eed8b8a55f</externalCheckSum>
        <floorplan>false</floorplan>
        <titlePicture>true</titlePicture>
        <urls>
            <url scale="SCALE" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/%WIDTH%x%HEIGHT%&gt;/format/jpg"/>
            <url scale="SCALE_AND_CROP" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/legacy_thumbnail/%WIDTH%x%HEIGHT%/format/jpg"/>
            <url scale="WHITE_FILLING" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/%WIDTH%x%HEIGHT%&gt;/extent/%WIDTH%x%HEIGHT%/format/jpg"/>
            <url scale="SCALE_540x540" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/540x540&gt;/format/jpg"/>
            <url scale="SCALE_210x210" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/210x210&gt;/format/jpg"/>
            <url scale="SCALE_400x300" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/400x300&gt;/format/jpg"/>
            <url scale="SCALE_118x118" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/118x118&gt;/extent/118x118/format/jpg"/>
            <url scale="SCALE_60x60" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/60x60&gt;/extent/60x60/format/jpg"/>
            <url scale="SCALE_73x73" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/legacy_thumbnail/73x73/format/jpg"/>
            <url scale="SCALE_1000x1000" href="https://pictures.sandbox-immobilienscout24.de/listings-test/703bbee7-d9f2-482e-b383-c3549f6b616b-904864036.jpg/ORIG/resize/1000x1000&gt;/format/jpg"/>
        </urls>
    </attachment>
    <attachment xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="common:Picture" xlink:href="https://rest.sandbox-immobilienscout24.de/restapi/api/offer/v1.0/user/me/realestate/325477914/attachment/904864037" id="904864037" modification="2026-09-29T23:13:43.892Z">
        <title>anonymized</title>
        <externalId>tf-att-2</externalId>
        <externalCheckSum>a2dabab4054e1cbc30c1bace49b073eed8b8a55f</externalCheckSum>
        <floorplan>false</floorplan>
        <titlePicture>false</titlePicture>
        <urls>
            <url scale="SCALE" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/%WIDTH%x%HEIGHT%&gt;/format/jpg"/>
            <url scale="SCALE_AND_CROP" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/legacy_thumbnail/%WIDTH%x%HEIGHT%/format/jpg"/>
            <url scale="WHITE_FILLING" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/%WIDTH%x%HEIGHT%&gt;/extent/%WIDTH%x%HEIGHT%/format/jpg"/>
            <url scale="SCALE_540x540" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/540x540&gt;/format/jpg"/>
            <url scale="SCALE_210x210" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/210x210&gt;/format/jpg"/>
            <url scale="SCALE_400x300" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/400x300&gt;/format/jpg"/>
            <url scale="SCALE_118x118" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/118x118&gt;/extent/118x118/format/jpg"/>
            <url scale="SCALE_60x60" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/60x60&gt;/extent/60x60/format/jpg"/>
            <url scale="SCALE_73x73" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/legacy_thumbnail/73x73/format/jpg"/>
            <url scale="SCALE_1000x1000" href="https://pictures.sandbox-immobilienscout24.de/listings-test/09b6c49b-87bd-41bf-ab66-e9d47f82ac3f-904864037.jpg/ORIG/resize/1000x1000&gt;/format/jpg"/>
        </urls>
    </attachment>
</common:attachments>
`
)

// fullPictureModel sets every attribute a picture has.
func fullPictureModel() *pictureModel {
	s := types.StringValue
	return &pictureModel{fileAttachmentModel: fileAttachmentModel{
		ID: s("904864036"), RealEstateID: s("325477914"), File: s("testdata/anonymized.jpg"),
		FileSHA256: s(anonymizedJPEGSHA256), ContentType: s("image/jpeg"), Title: s("anonymized"),
		ExternalID: s("tf-att-1"), Floorplan: types.BoolValue(false),
	}, TitlePicture: types.BoolValue(true)}
}

func fullLinkModel() *linkModel {
	s := types.StringValue
	return &linkModel{ID: s("904864040"), RealEstateID: s("325477914"), URL: s("https://www.immobilienscout24.de"),
		Title: s("anonymized"), ExternalID: s("tf-att-link")}
}

// Every type is sent in the order of its schema, with every modelled field,
// in the prefixed form of the observed requests, and the fake agrees.
func TestAttachmentMetadataFollowsTheSchemaOrder(t *testing.T) {
	f := newFakeAPI(t)
	picture := fullPictureModel()
	for name, tc := range map[string]struct {
		doc      *attachmentDocument
		observed string
		want     []string
	}{
		"picture": {picture.toDocument(pictureKind, anonymizedJPEGSHA256, false), observedPictureMetadata,
			[]string{"title", "externalId", "externalCheckSum", "floorplan", "titlePicture"}},
		"PDF document": {picture.toDocument(pdfKind, anonymizedPDFSHA256, true), observedPDFMetadata,
			[]string{"title", "externalId", "externalCheckSum", "floorplan"}},
		"link": {fullLinkModel().toDocument(), observedLinkMetadata, []string{"title", "externalId", "url"}},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := marshalAttachment(tc.doc)
			if err != nil {
				t.Fatal(err)
			}
			root, names := topLevelChildren(t, body)
			if root.Space != commonNamespace || root.Local != "attachment" {
				t.Fatalf("root = %+v", root)
			}
			// The observed requests open the same way.
			open := `<common:attachment xsi:type="common:` + tc.doc.Type + `" xmlns:common="` + commonNamespace +
				`" xmlns:xlink="` + xlinkNamespace + `" xmlns:xsi="` + xsiNamespace + `">`
			if !bytes.Contains(body, []byte(open)) || !strings.HasPrefix(tc.observed, open) {
				t.Fatalf("root is not in the observed form %s:\n%s", open, body)
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("elements = %v, want %v", names, tc.want)
			}
			if _, observed := topLevelChildren(t, []byte(tc.observed)); !slices.Equal(observed, tc.want) {
				t.Fatalf("the observed request has %v, want %v", observed, tc.want)
			}
			for _, b := range [][]byte{body, []byte(tc.observed)} {
				if _, _, err := f.parseMetadata(b); err != nil {
					t.Fatalf("fake API rejects:\n%s\n%v", b, err)
				}
			}
		})
	}
	// titlePicture is always sent for a picture, true only when asked for.
	for makeTitle, want := range map[bool]string{true: "<titlePicture>true</titlePicture>", false: "<titlePicture>false</titlePicture>"} {
		body, _ := marshalAttachment(picture.toDocument(pictureKind, anonymizedJPEGSHA256, makeTitle))
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("makeTitle %v: body lacks %s:\n%s", makeTitle, want, body)
		}
	}
}

// The upload is the observed two-part request, metadata first, and the id
// comes from the observed answer.
func TestUploadRequestShape(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Location", observedAttachmentLocation)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, observedAttachmentCreatedBody)
	}))
	defer srv.Close()
	jpeg := readTestdata(t, "anonymized.jpg")
	doc := fullPictureModel().toDocument(pictureKind, anonymizedJPEGSHA256, false)
	metadata, err := marshalAttachment(doc)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(srv.URL+"/restapi/api", "ck", "cs", "at", "ats", "test")
	id, err := c.UploadAttachment(context.Background(), "325477914", doc,
		attachmentFile{Name: uploadFileName("testdata/anonymized.jpg"), ContentType: "image/jpeg", Content: jpeg})
	if err != nil || id != "904864036" {
		t.Fatalf("id = %q, err = %v", id, err)
	}

	if got.Method != http.MethodPost || got.URL.Path != "/restapi/api/offer/v1.0/user/me/realestate/325477914/attachment" ||
		got.Header.Get("Accept") != mediaTypeXML || !strings.HasPrefix(got.Header.Get("Authorization"), "OAuth ") {
		t.Fatalf("request %s %s, Accept %q, Authorization %q", got.Method, got.URL.Path, got.Header.Get("Accept"), got.Header.Get("Authorization"))
	}
	mt, params, err := mime.ParseMediaType(got.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q", got.Header.Get("Content-Type"))
	}
	want := []struct {
		header  textproto.MIMEHeader
		content []byte
	}{
		{textproto.MIMEHeader{
			"Content-Disposition":       {`form-data; name="metadata"; filename="body.xml"`},
			"Content-Type":              {"application/xml; name=body.xml"},
			"Content-Transfer-Encoding": {"binary"},
		}, metadata},
		{textproto.MIMEHeader{
			"Content-Disposition":       {`form-data; name="attachment"; filename="anonymized.jpg"`},
			"Content-Type":              {"image/jpeg; name=anonymized.jpg"},
			"Content-Transfer-Encoding": {"binary"},
		}, jpeg},
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for i := 0; ; i++ {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			if i != len(want) {
				t.Fatalf("%d parts, want %d", i, len(want))
			}
			break
		}
		if err != nil || i >= len(want) {
			t.Fatalf("part %d: %v", i, err)
		}
		if !equalHeaders(p.Header, want[i].header) {
			t.Errorf("part %d headers = %v, want %v", i, p.Header, want[i].header)
		}
		if content, err := io.ReadAll(p); err != nil || !bytes.Equal(content, want[i].content) {
			t.Errorf("part %d content differs (%v): %d bytes, want %d", i, err, len(content), len(want[i].content))
		}
	}
	for _, s := range [][]byte{metadata, jpeg} {
		if !bytes.Contains(body, s) {
			t.Fatal("the body does not carry the metadata and the file as they are")
		}
	}
}

func equalHeaders(a, b textproto.MIMEHeader) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// readTestdata returns the content of a file in testdata.
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUnmarshalAttachmentParsesObservedBodies(t *testing.T) {
	for id, wantTitle := range map[string]bool{"904864036": true, "904864037": false} {
		doc, err := unmarshalAttachment(id, []byte(observedAttachmentListBody))
		if err != nil {
			t.Fatal(err)
		}
		if doc.ID != id || doc.Type != attachmentPicture || doc.Title != "anonymized" ||
			doc.ExternalCheckSum != "a2dabab4054e1cbc30c1bace49b073eed8b8a55f" || doc.Floorplan == nil || *doc.Floorplan ||
			doc.TitlePicture == nil || *doc.TitlePicture != wantTitle || doc.URL != "" {
			t.Errorf("%s: %+v", id, doc)
		}
		m, err := pictureKindResource().fromDocument(doc, &pictureModel{fileAttachmentModel: fileAttachmentModel{
			ID: types.StringValue(id), RealEstateID: types.StringValue("325477914")}})
		if err != nil || !m.FileSHA256.Equal(types.StringValue(doc.ExternalCheckSum)) || m.TitlePicture.ValueBool() != wantTitle {
			t.Errorf("%s: model %+v, %v", id, m, err)
		}
	}
	// One attachment as its own document, the shape of the documented GET.
	start := strings.Index(observedAttachmentListBody, "    <attachment ")
	end := strings.Index(observedAttachmentListBody, "    </attachment>")
	single := `<common:attachment ` + observedAttachmentNamespaces +
		strings.TrimPrefix(observedAttachmentListBody[start:end], "    <attachment") + "</common:attachment>"
	if doc, err := unmarshalAttachment("904864036", []byte(single)); err != nil || doc.ID != "904864036" || !*doc.TitlePicture {
		t.Fatalf("single attachment: %+v, %v\n%s", doc, err, single)
	}
	for name, tc := range map[string]struct{ id, body string }{
		"another attachment": {"904864036", strings.Replace(single, `id="904864036"`, `id="904864037"`, 1)},
		"not in the list":    {"904864099", observedAttachmentListBody},
		"a messages body":    {"904864036", observedAttachmentCreatedBody},
	} {
		if doc, err := unmarshalAttachment(tc.id, []byte(tc.body)); err == nil {
			t.Errorf("%s: parsed as %+v", name, doc)
		}
	}
	pdf := `<common:attachment xsi:type="common:PDFDocument" xmlns:common="` + commonNamespace + `" xmlns:xsi="` + xsiNamespace +
		`" id="1"><title>t</title><url>https://d12ts8pcffi7qk.cloudfront.net/x.pdf</url><floorplan>true</floorplan></common:attachment>`
	doc, err := unmarshalAttachment("1", []byte(pdf))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pictureKindResource().fromDocument(doc, &pictureModel{}); err == nil ||
		!strings.Contains(err.Error(), "import it as immobilienscout24_attachment_pdf") {
		t.Fatalf("a PDF document read as a picture: %v", err)
	}
}

func pictureKindResource() *fileAttachmentResource { return &fileAttachmentResource{kind: pictureKind} }

func TestAttachmentContentTypeFromExtension(t *testing.T) {
	for name, want := range map[string]string{
		"a.jpg": "image/jpeg", "b/c.JPG": "image/jpeg", "d.jpeg": "image/jpeg", "e.png": "image/png", "f.gif": "image/gif",
		"g.pdf": "", "h.bmp": "", "i.webp": "", "jpg": "", "j.jpg.txt": "",
	} {
		if got, ok := pictureKind.contentType(name); got != want || ok != (want != "") {
			t.Errorf("picture %q: %q, %v; want %q", name, got, ok, want)
		}
	}
	for name, want := range map[string]string{"a.pdf": "application/pdf", "B.PDF": "application/pdf", "c.jpg": "", "pdf": ""} {
		if got, ok := pdfKind.contentType(name); got != want || ok != (want != "") {
			t.Errorf("PDF %q: %q, %v; want %q", name, got, ok, want)
		}
	}
	if got := pictureKind.extensions(); got != ".gif, .jpeg, .jpg, .png" {
		t.Errorf("extensions = %q", got)
	}
}

func TestFileChecksAndChecksum(t *testing.T) {
	if sum, err := fileSHA256("testdata/anonymized.jpg"); err != nil || sum != anonymizedJPEGSHA256 {
		t.Fatalf("sha256 = %q, %v", sum, err)
	}
	if sum, err := fileSHA256("testdata/anonymized.pdf"); err != nil || sum != anonymizedPDFSHA256 {
		t.Fatalf("sha256 = %q, %v", sum, err)
	}
	dir := t.TempDir()
	empty, large := filepath.Join(dir, "empty.jpg"), filepath.Join(dir, "large.jpg")
	for _, err := range []error{os.WriteFile(empty, nil, 0o600), os.WriteFile(large, nil, 0o600), os.Truncate(large, maxFileBytes+1)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		filepath.Join(dir, "missing.jpg"): "no such file", dir: "is not a regular file", empty: "is empty",
		large: "more than the 50 MB",
	} {
		if _, err := fileSHA256(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("fileSHA256(%s) = %v, want %q", name, err, want)
		}
		if _, err := readFile(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("readFile(%s) = %v, want %q", name, err, want)
		}
	}
	if err := os.Truncate(large, maxFileBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := fileSHA256(large); err != nil {
		t.Errorf("a file of exactly 50 MiB: %v", err)
	}
}

func TestUploadFileName(t *testing.T) {
	for path, want := range map[string]string{
		"/home/anna/pictures/anonymized.jpg": "anonymized.jpg",
		"Küche 1.JPG":                        "K_che_1.JPG",
		`a"b;c.png`:                          "a_b_c.png",
		"grundriss_eg-2.pdf":                 "grundriss_eg-2.pdf",
	} {
		if got := uploadFileName(path); got != want {
			t.Errorf("uploadFileName(%q) = %q, want %q", path, got, want)
		}
	}
}

// A link is a plain XML POST; reading, updating and deleting use the
// attachment's path below its real estate.
func TestAttachmentRequestPaths(t *testing.T) {
	type seen struct{ method, path, accept, contentType string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.Method, r.URL.Path, r.Header.Get("Accept"), r.Header.Get("Content-Type")})
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, strings.ReplaceAll(observedAttachmentCreatedBody, "904864036", "904864040"))
		case http.MethodGet:
			_, _ = io.WriteString(w, observedAttachmentListBody)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/restapi/api/", "ck", "cs", "at", "ats", "test")
	ctx := context.Background()
	if id, err := c.CreateAttachment(ctx, "325477914", fullLinkModel().toDocument()); err != nil || id != "904864040" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
	_, _ = c.GetAttachment(ctx, "325477914", "904864036")
	_ = c.UpdateAttachment(ctx, "325477914", "904864040", fullLinkModel().toDocument())
	_ = c.DeleteAttachment(ctx, "325477914", "904864040")
	base := "/restapi/api/offer/v1.0/user/me/realestate/325477914/attachment"
	want := []seen{
		{"POST", base, "application/xml", "application/xml"},
		{"GET", base + "/904864036", "application/xml", ""},
		{"PUT", base + "/904864040", "application/xml", "application/xml"},
		{"DELETE", base + "/904864040", "application/xml", ""},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("requests = %+v, want %+v", got, want)
	}
}

// The id comes from the body, else from the Location header, else from the
// message text; without any, the error says that the attachment may exist.
func TestCreateAttachmentParsesObservedResponse(t *testing.T) {
	ctx := context.Background()
	doc := fullLinkModel().toDocument()
	noID := strings.Replace(observedAttachmentCreatedBody, "<id>904864036</id>", "", 1)
	for name, tc := range map[string]struct {
		location, body string
	}{
		"body":         {"", observedAttachmentCreatedBody},
		"Location":     {observedAttachmentLocation, strings.Replace(noID, "with id [904864036]", "with id []", 1)},
		"message text": {"", noID},
	} {
		c := stubServer(t, http.StatusCreated, http.Header{"Location": {tc.location}}, tc.body)
		if id, err := c.CreateAttachment(ctx, "325477914", doc); err != nil || id != "904864036" {
			t.Errorf("%s: id = %q, err = %v", name, id, err)
		}
	}
	c := stubServer(t, http.StatusCreated, nil, "Created")
	if id, err := c.CreateAttachment(ctx, "325477914", doc); err == nil || !strings.Contains(err.Error(), "probably exists on the listing") {
		t.Fatalf("got id %q, err %v; want an error", id, err)
	}
}
