package immobilienscout24

// The request and response bodies of the attachments of the fake API, see
// fake_api_attachment_test.go.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// fakePictureScales are the scales of the picture URLs the sandbox returned,
// with the end of each URL.
var fakePictureScales = [][2]string{
	{"SCALE", "resize/%WIDTH%x%HEIGHT%>/format/jpg"},
	{"SCALE_AND_CROP", "legacy_thumbnail/%WIDTH%x%HEIGHT%/format/jpg"},
	{"WHITE_FILLING", "resize/%WIDTH%x%HEIGHT%>/extent/%WIDTH%x%HEIGHT%/format/jpg"},
	{"SCALE_540x540", "resize/540x540>/format/jpg"},
	{"SCALE_210x210", "resize/210x210>/format/jpg"},
	{"SCALE_400x300", "resize/400x300>/format/jpg"},
	{"SCALE_118x118", "resize/118x118>/extent/118x118/format/jpg"},
	{"SCALE_60x60", "resize/60x60>/extent/60x60/format/jpg"},
	{"SCALE_73x73", "legacy_thumbnail/73x73/format/jpg"},
	{"SCALE_1000x1000", "resize/1000x1000>/format/jpg"},
}

// fakeLinkPattern is the pattern of a link's url in IS24's per-file XSD,
// anchored as XSD patterns are.
var fakeLinkPattern = regexp.MustCompile(`^(http|https)://\w.*[.]\w.*$`)

// fakeXMLEscaper escapes text the way the sandbox does, apostrophes as they are.
var fakeXMLEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// fakeSchemaError is a schema violation, which the sandbox answers with 412
// ERROR_COMMON_SCHEMA_VALIDATION_FAILED and the validator's message.
type fakeSchemaError struct{ message string }

func (e fakeSchemaError) Error() string {
	return "The request is not schema valid. [MESSAGE: " + e.message + "]"
}

var errFakeMediaType = errors.New("unsupported media type")

// parseUpload checks a multipart upload: a part named metadata and one named
// attachment, in either order, and nothing else.
func (f *fakeAPI) parseUpload(body []byte, boundary string) (*fakeAttachment, bool, error) {
	if boundary == "" {
		return nil, false, errors.New("fake API: multipart/form-data without a boundary")
	}
	parts := map[string][]byte{}
	fileType, uploadName := "", ""
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("fake API: malformed multipart body: %v", err)
		}
		name := p.FormName()
		content, err := io.ReadAll(p)
		if err != nil {
			return nil, false, fmt.Errorf("fake API: reading part %q: %v", name, err)
		}
		if _, dup := parts[name]; dup || (name != "metadata" && name != "attachment") {
			return nil, false, fmt.Errorf("fake API: unexpected part %q; an upload has one part named metadata and one named attachment", name)
		}
		// The Insert page asks for these headers on both parts.
		mediaType, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		switch {
		case p.Header.Get("Content-Transfer-Encoding") != "binary":
			return nil, false, fmt.Errorf("fake API: part %q lacks Content-Transfer-Encoding: binary", name)
		case name == "metadata" && mediaType != mediaTypeXML:
			return nil, false, fmt.Errorf("fake API: the metadata part has Content-Type %q, want application/xml", mediaType)
		case name == "attachment" && p.FileName() == "":
			return nil, false, errors.New("fake API: the attachment part has no filename")
		case name == "attachment":
			fileType, uploadName = mediaType, p.FileName()
		}
		parts[name] = content
	}
	metadata, file := parts["metadata"], parts["attachment"]
	if metadata == nil || file == nil {
		return nil, false, errors.New("fake API: an upload needs a part named metadata and one named attachment")
	}
	a, makeTitle, err := f.parseMetadata(metadata)
	if err != nil {
		return nil, false, err
	}
	switch {
	case a.xsiType == attachmentLink:
		return nil, false, errors.New("fake API: a common:Link takes a plain XML body, not an upload")
	case a.xsiType == attachmentPicture && !strings.HasPrefix(fileType, "image/"),
		a.xsiType == attachmentPDF && fileType != "application/pdf":
		return nil, false, fmt.Errorf("fake API: a common:%s cannot be a file of type %q", a.xsiType, fileType)
	case len(file) == 0:
		return nil, false, errors.New("fake API: the file is empty")
	}
	sum := sha256.Sum256(file)
	a.contentSHA256, a.contentType = hex.EncodeToString(sum[:]), fileType
	// Observed on the sandbox (2026-09-30): a file uploaded without a title
	// gets its file name without the extension, read as Latin-1 and with
	// underscores as spaces, so a UTF-8 "Küche 1.jpg" becomes "KÃ¼che 1" and
	// "K_che_1.jpg" becomes "K che 1".
	if a.fields["title"] == "" {
		a.fields["title"] = strings.ReplaceAll(latin1(strings.TrimSuffix(uploadName, path.Ext(uploadName))), "_", " ")
	}
	return a, makeTitle, nil
}

// parseMetadata checks attachment metadata against the schema of its type, as
// the sandbox does, and returns it, and whether it sets titlePicture true.
func (f *fakeAPI) parseMetadata(body []byte) (*fakeAttachment, bool, error) {
	root, err := parseRequest(body, commonNamespace, "attachment")
	if err != nil {
		return nil, false, err
	}
	typ := ""
	for _, a := range root.Attrs {
		if a.Name.Space == xsiNamespace && a.Name.Local == "type" {
			typ = a.Value
		}
	}
	local, prefixed := strings.CutPrefix(typ, "common:")
	sequence, known := f.att.sequences[local]
	if !prefixed || !known {
		return nil, false, fmt.Errorf("fake API: xsi:type %q is not common:Picture, common:PDFDocument or common:Link", typ)
	}
	if err := checkSchemaSequence(childNames(root), sequence); err != nil {
		return nil, false, err
	}
	a := &fakeAttachment{xsiType: local, fields: map[string]string{}}
	makeTitle := false
	for _, c := range root.Children {
		text := strings.TrimSpace(c.Text)
		if err := checkAttachmentValue(local, c.Name, text); err != nil {
			return nil, false, err
		}
		switch {
		case c.Name == "titlePicture":
			makeTitle = text == "true" || text == "1"
		case c.Name == "checkSum" || c.Name == "urls" || (c.Name == "url" && local == attachmentPDF):
			// Server-owned.
		default:
			a.fields[c.Name] = text
		}
	}
	return a, makeTitle, nil
}

// checkSchemaSequence checks the children of a common:attachment against a
// sequence, and fails the way the sandbox's schema validator does: with the
// element out of place, or the end of the content, and what may come there.
func checkSchemaSequence(children []string, sequence []xsdElement) error {
	next := 0
	for _, c := range children {
		i := next
		for i < len(sequence) && sequence[i].Name != c && sequence[i].Optional {
			i++
		}
		switch {
		case next == len(sequence):
			return fakeSchemaError{"cvc-complex-type.2.4.d: Invalid content was found starting with element '" + c +
				"'. No child element is expected at this point."}
		case i == len(sequence) || sequence[i].Name != c:
			return fakeSchemaError{"cvc-complex-type.2.4.a: Invalid content was found starting with element '" + c +
				"'. One of '{" + expectedAt(sequence, next) + "}' is expected."}
		}
		next = i + 1
	}
	for i := next; i < len(sequence); i++ {
		if !sequence[i].Optional {
			return fakeSchemaError{"cvc-complex-type.2.4.b: The content of element 'common:attachment' is not complete. " +
				"One of '{" + expectedAt(sequence, next) + "}' is expected."}
		}
	}
	return nil
}

// expectedAt lists the elements that may come at position i of a sequence:
// the optional ones up to the first mandatory one, which is included.
func expectedAt(sequence []xsdElement, i int) string {
	var names []string
	for ; i < len(sequence); i++ {
		names = append(names, sequence[i].Name)
		if !sequence[i].Optional {
			break
		}
	}
	return strings.Join(names, ", ")
}

// checkAttachmentValue checks the value of an element. Simulated: the
// sandbox was not seen to refuse any value.
func checkAttachmentValue(typ, name, text string) error {
	tooLong := func(limit int) error {
		if n := utf8.RuneCountInString(text); n > limit {
			return fakeSchemaError{fmt.Sprintf("cvc-maxLength-valid: Value '%s' with length = '%d' is not facet-valid "+
				"with respect to maxLength '%d'.", text, n, limit)}
		}
		return nil
	}
	switch {
	case name == "floorplan" || name == "titlePicture":
		if !slices.Contains([]string{"true", "false", "1", "0"}, text) {
			return fakeSchemaError{"cvc-datatype-valid.1.2.1: '" + text + "' is not a valid value for 'boolean'."}
		}
	case name == "title":
		return tooLong(30)
	case name == "externalId":
		return tooLong(50)
	case name == "url" && typ == attachmentLink:
		if !fakeLinkPattern.MatchString(text) {
			return fakeSchemaError{"cvc-pattern-valid: Value '" + text + "' is not facet-valid with respect to pattern " +
				"'(http|https)://\\w.*[.]\\w.*'."}
		}
		return tooLong(2000)
	}
	return nil
}

func writeAttachmentError(w http.ResponseWriter, err error) {
	var schemaErr fakeSchemaError
	switch {
	case errors.As(err, &schemaErr):
		writeAttachmentMessage(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", schemaErr.Error(), "")
	case errors.Is(err, errFakeMediaType):
		writeMessages(w, http.StatusUnsupportedMediaType, "ERROR_COMMON_MEDIA_TYPE_UNSUPPORTED", "Unsupported media type.")
	default:
		// Simulated.
		writeMessages(w, http.StatusBadRequest, "ERROR_COMMON_BAD_REQUEST", err.Error())
	}
}

// writeAttachmentMessage writes a <common:messages> body in the layout of the
// sandbox's answers to attachment requests, with an <id> unless id is empty.
func writeAttachmentMessage(w http.ResponseWriter, status int, code, text, id string) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<common:messages ` + observedAttachmentNamespaces + ">\n    <message>\n")
	b.WriteString("        <messageCode>" + code + "</messageCode>\n")
	b.WriteString("        <message>" + fakeXMLEscaper.Replace(text) + "</message>\n")
	if id != "" {
		b.WriteString("        <id>" + id + "</id>\n")
	}
	b.WriteString("    </message>\n</common:messages>\n")
	writeRaw(w, status, b.String())
}

// renderAttachment is the GET body of an attachment. The caller holds f.mu.
func (f *fakeAPI) renderAttachment(id string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	f.writeAttachment(&b, id, "common:attachment", " "+observedAttachmentNamespaces, "")
	return b.String()
}

// renderAttachmentList is the GET body of the attachments of a real estate
// that keep accepts: the pictures and PDF documents in the attachment order,
// then the links. The caller holds f.mu.
func (f *fakeAPI) renderAttachmentList(realEstateID string, keep func(*fakeAttachment) bool) string {
	ids := slices.Clone(f.att.order[realEstateID])
	var links []string
	for id, a := range f.att.attachments {
		if a.realEstateID == realEstateID && a.xsiType == attachmentLink {
			links = append(links, id)
		}
	}
	sort.Strings(links)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<common:attachments ` + observedAttachmentNamespaces + ">\n")
	for _, id := range append(ids, links...) {
		if keep(f.att.attachments[id]) {
			f.writeAttachment(&b, id, "attachment", "", "    ")
		}
	}
	b.WriteString("</common:attachments>\n")
	return b.String()
}

// renderAttachmentOrder is the GET body of the attachment order of a real
// estate. The caller holds f.mu.
func (f *fakeAPI) renderAttachmentOrder(realEstateID string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<ns8:attachmentsorder ` + observedAttachmentNamespaces + ">\n")
	for _, id := range f.att.order[realEstateID] {
		b.WriteString("    <attachmentId>" + id + "</attachmentId>\n")
	}
	b.WriteString("</ns8:attachmentsorder>\n")
	return b.String()
}

// writeAttachment writes an attachment as the element name, with attrs before
// the attributes every attachment has, in the sandbox's layout. The caller
// holds f.mu.
func (f *fakeAPI) writeAttachment(b *strings.Builder, id, name, attrs, indent string) {
	a := f.att.attachments[id]
	b.WriteString(indent + "<" + name + attrs + ` xmlns:xsi="` + xsiNamespace + `" xsi:type="common:` + a.xsiType +
		`" xlink:href="` + f.BaseURL() + "/offer/v1.0/user/me/realestate/" + a.realEstateID + "/attachment/" + id +
		`" id="` + id + `" modification="` + a.modification + `">` + "\n")
	line := func(element, text string) {
		b.WriteString(indent + "    <" + element + ">" + fakeXMLEscaper.Replace(text) + "</" + element + ">\n")
	}
	for _, e := range f.att.sequences[a.xsiType] {
		switch {
		case e.Name == "titlePicture":
			line(e.Name, strconv.FormatBool(f.titlePicture(a.realEstateID) == id))
		case e.Name == "urls":
			sum := sha256.Sum256([]byte(id))
			h := hex.EncodeToString(sum[:16])
			file := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:] + "-" + id + ".jpg/ORIG/"
			b.WriteString(indent + "    <urls>\n")
			for _, s := range fakePictureScales {
				b.WriteString(indent + `        <url scale="` + s[0] + `" href="https://pictures.sandbox-immobilienscout24.de/listings-test/` +
					file + fakeXMLEscaper.Replace(s[1]) + `"/>` + "\n")
			}
			b.WriteString(indent + "    </urls>\n")
		case e.Name == "url" && a.xsiType == attachmentPDF:
			line(e.Name, "https://d12ts8pcffi7qk.cloudfront.net/listings-test/"+id+".pdf")
		default:
			if v, ok := a.fields[e.Name]; ok {
				line(e.Name, v)
			}
		}
	}
	b.WriteString(indent + "</" + name + ">\n")
}

// latin1 reads the bytes of s as Latin-1, as the sandbox reads an upload's
// file name.
func latin1(s string) string {
	r := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		r[i] = rune(s[i])
	}
	return string(r)
}
