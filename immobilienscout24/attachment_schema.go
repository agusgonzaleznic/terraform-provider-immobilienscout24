package immobilienscout24

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// maxFileBytes is the documented limit for pictures and PDF documents, "The
// maximum file size is 50 MB", read as 50 MiB.
const maxFileBytes = 50 << 20

// attachmentLimits is the documented number of attachments a listing takes.
const attachmentLimits = "ImmobilienScout24 accepts up to 150 pictures and PDF documents per listing, each at most " +
	"50 MB, and up to 150 links."

// fileKind describes the attachment type that a fileAttachmentResource manages.
type fileKind struct {
	typeName    string // the resource type name after the provider's
	xsiType     string
	noun        string
	description string
	// contentTypes maps a lower-case file name extension to its media type.
	contentTypes map[string]string
	// contentTypesDoc describes contentTypes in Markdown.
	contentTypesDoc string
	// exampleFile is a file name for the documentation.
	exampleFile string
	// titlePicture tells whether the type has the titlePicture element.
	titlePicture bool
}

var pictureKind = &fileKind{
	typeName: "_attachment_picture",
	xsiType:  attachmentPicture,
	noun:     "picture",
	description: "A picture (`common:Picture`) of a real estate listing, uploaded from a local file.\n\n" +
		"ImmobilienScout24 cannot change the file of an attachment, so new content in `file` replaces the picture, " +
		"which gets a new id. `title`, `external_id`, `floorplan` and `title_picture` change in place.\n\n" +
		"A listing with pictures has exactly one title picture, the first picture in its attachment order. The first " +
		"picture uploaded becomes the title picture, also when it does not set `title_picture`, and deleting the " +
		"title picture makes the next picture the title picture. `title_picture` explains how to choose it. " +
		attachmentLimits,
	contentTypes:    map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif"},
	contentTypesDoc: "`image/jpeg` for `.jpg` and `.jpeg`, `image/png` for `.png` and `image/gif` for `.gif`",
	exampleFile:     "living-room.jpg",
	titlePicture:    true,
}

var pdfKind = &fileKind{
	typeName: "_attachment_pdf",
	xsiType:  attachmentPDF,
	noun:     "PDF document",
	description: "A PDF document (`common:PDFDocument`) of a real estate listing, such as a floor plan or an " +
		"energy certificate, uploaded from a local file.\n\n" +
		"ImmobilienScout24 cannot change the file of an attachment, so new content in `file` replaces the document, " +
		"which gets a new id. `title`, `external_id` and `floorplan` change in place. ImmobilienScout24 scans " +
		"documents for malware, and removes files that fail the scan or are password protected. " + attachmentLimits,
	contentTypes:    map[string]string{".pdf": "application/pdf"},
	contentTypesDoc: "`application/pdf` for `.pdf`",
	exampleFile:     "floor-plan.pdf",
}

// contentType returns the media type for the extension of name.
func (k *fileKind) contentType(name string) (string, bool) {
	ct, ok := k.contentTypes[strings.ToLower(filepath.Ext(name))]
	return ct, ok
}

// extensions lists the extensions that contentType knows, such as ".gif, .jpeg".
func (k *fileKind) extensions() string {
	var exts []string
	for ext := range k.contentTypes {
		exts = append(exts, ext)
	}
	slices.Sort(exts)
	return strings.Join(exts, ", ")
}

// checkFile checks what the API documents for a file before it is read.
func checkFile(name string, info os.FileInfo) error {
	switch {
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", name)
	case info.Size() == 0:
		return fmt.Errorf("%s is empty", name)
	case info.Size() > maxFileBytes:
		return fmt.Errorf("%s has %d bytes, more than the 50 MB that ImmobilienScout24 accepts for a file", name, info.Size())
	}
	return nil
}

// fileSHA256 returns the SHA-256 of a file's content in lower-case hex. It
// checks the file before opening it, since opening a FIFO or a device blocks,
// and reads at most the 50 MB the API accepts.
func fileSHA256(name string) (string, error) {
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if err := checkFile(name, info); err != nil {
		return "", err
	}
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	if n > maxFileBytes {
		return "", fmt.Errorf("%s grew past the 50 MB that ImmobilienScout24 accepts for a file while it was read", name)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readFile reads a file for upload, after the checks of fileSHA256.
func readFile(name string) ([]byte, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if err := checkFile(name, info); err != nil {
		return nil, err
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	if len(content) > maxFileBytes {
		return nil, fmt.Errorf("%s grew past the 50 MB that ImmobilienScout24 accepts for a file while it was read", name)
	}
	return content, nil
}

// mediaTypeCheck accepts a media type such as image/jpeg, without parameters.
var mediaTypeCheck = stringCheck{
	description: "value must be a media type such as image/jpeg",
	summary:     "Invalid content type",
	check: func(s string) error {
		mt, params, err := mime.ParseMediaType(s)
		if err != nil || len(params) > 0 || mt != s || !strings.Contains(mt, "/") {
			return fmt.Errorf("%q is not a media type in lower case without parameters, such as image/jpeg", s)
		}
		return nil
	},
}

// Attributes that every attachment has.

func attachmentIDAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: "The attachment id that ImmobilienScout24 assigned on creation.",
		Computed:            true,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func attachmentRealEstateIDAttribute(noun string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: "The scout object id of the listing: the `id` of any listing resource, for example " +
			"`immobilienscout24_apartment_rent.example.id`. Changing it forces a new " + noun + ".",
		Required:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		Validators: []validator.String{
			stringvalidator.RegexMatches(positiveID, "must be a whole number in digits, without a leading zero"),
		},
	}
}

// maxAttachmentTitle is the most characters a title may have (docs).
const maxAttachmentTitle = 30

// attachmentTitleAttribute is Optional and Computed: an attachment uploaded
// without a title gets one (observed 2026-09-30), the file name without its
// extension for a file and "Link" for a link.
func attachmentTitleAttribute(noun, whenOmitted string) schema.StringAttribute {
	a := optionalStringWithMax("Title of the "+noun+" (`title`), shown on the listing, at most 30 characters. "+
		"When left out, the title is "+whenOmitted+". Removing it from the configuration later keeps the "+
		"current title.", maxAttachmentTitle)
	a.Computed = true
	a.PlanModifiers = []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	return a
}

func attachmentExternalIDAttribute() schema.StringAttribute {
	return optionalStringWithMax("Your own id for the attachment (`externalId`), at most 50 characters.", 50)
}

// fileAttachmentSchema is the schema of immobilienscout24_attachment_picture
// and immobilienscout24_attachment_pdf. See attachment_xml.go for the wire order.
func fileAttachmentSchema(kind *fileKind) schema.Schema {
	s := schema.Schema{
		MarkdownDescription: kind.description,
		Attributes: map[string]schema.Attribute{
			"id":             attachmentIDAttribute(),
			"real_estate_id": attachmentRealEstateIDAttribute(kind.noun),
			"file": schema.StringAttribute{
				MarkdownDescription: "Path of the local file, for example `\"${path.module}/files/" + kind.exampleFile + "\"`. " +
					"It is read at plan time and must exist then, and must not change before the apply: Terraform then " +
					"stops with \"Provider produced inconsistent final plan\" and nothing is uploaded. Once the " +
					"attachment is uploaded, a missing file keeps it as it is, with a warning, so that a destroy still " +
					"works. Only its content counts, see `file_sha256`: a new " +
					"path to the same content updates the attachment in place, without an upload. ImmobilienScout24 " +
					"does not return the file, so an imported attachment needs `file` in its configuration like any " +
					"other; the import leaves it empty in the state until the next apply.",
				Required:   true,
				Validators: []validator.String{nonEmpty},
			},
			"file_sha256": schema.StringAttribute{
				MarkdownDescription: "The SHA-256 of the file's content in lower-case hex, computed at plan time. The " +
					"provider stores it as the attachment's `externalCheckSum` on ImmobilienScout24 and reads it back " +
					"from there, so new content in the file, or another checksum set outside Terraform, replaces the " +
					"attachment. An imported attachment keeps its id only if its `externalCheckSum` already is the " +
					"SHA-256 of the configured file; one uploaded by other software usually has another checksum or " +
					"none, and is uploaded again on the next apply.",
				Computed: true,
			},
			"content_type": schema.StringAttribute{
				MarkdownDescription: "The media type the file is uploaded with. Derived from the extension of `file` " +
					"when not set: " + kind.contentTypesDoc + ". Set it for a file with another extension. " +
					"ImmobilienScout24 does not return it, so an import leaves it empty until the next apply. " +
					"Changing it replaces the attachment.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{mediaTypeCheck},
			},
			"title":       attachmentTitleAttribute(kind.noun, "the file's name without its extension, cut to 30 characters, so choose file names that can be shown publicly"),
			"external_id": attachmentExternalIDAttribute(),
			"floorplan": schema.BoolAttribute{
				MarkdownDescription: "Whether the " + kind.noun + " is a floor plan (`floorplan`). Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
	if kind.titlePicture {
		s.Attributes["title_picture"] = schema.BoolAttribute{
			MarkdownDescription: "Whether this is the listing's title picture (`titlePicture`). A listing with pictures " +
				"has exactly one. Set it to `true` to make this picture the title picture; the previous one loses the " +
				"flag. It cannot be `false`: ImmobilienScout24 ignores that, and the title picture only changes when " +
				"another picture becomes the title picture or the title picture is deleted. When left out, the flag " +
				"is read back but not managed, and a plan that changes the picture shows it as known after apply, " +
				"because another picture can take the flag or pass it on in the same run. Set it to `true` on one " +
				"picture per listing only: when two pictures set it, an apply that writes both fails with \"Two " +
				"pictures set title_picture = true\", and any other apply gives the flag back to the picture that " +
				"lost it in the apply before.",
			Optional:   true,
			Computed:   true,
			Validators: []validator.Bool{titlePictureOnlyTrue},
		}
	}
	return s
}

// titlePictureOnlyTrue rejects title_picture = false, which ImmobilienScout24
// ignores on the title picture.
var titlePictureOnlyTrue = onlyTrue{
	summary: "title_picture cannot be false",
	detail: "A listing with pictures always has exactly one title picture, and ImmobilienScout24 ignores " +
		"titlePicture false. To choose the title picture, set title_picture = true on that " +
		"immobilienscout24_attachment_picture, and leave title_picture out of the others.",
}

// linkSchema is the schema of immobilienscout24_attachment_link.
func linkSchema() schema.Schema {
	return schema.Schema{
		MarkdownDescription: "A link (`common:Link`) of a real estate listing, for example to a video or a virtual " +
			"tour. ImmobilienScout24 shows a virtual tour of a provider it supports as an interactive tour, and " +
			"any other URL in the links section of the listing. Links are not part of the attachment order, which " +
			"only holds pictures and PDF documents. Every argument but `real_estate_id` changes in place. " +
			attachmentLimits,
		Attributes: map[string]schema.Attribute{
			"id":             attachmentIDAttribute(),
			"real_estate_id": attachmentRealEstateIDAttribute("link"),
			"url": schema.StringAttribute{
				MarkdownDescription: "The URL (`url`), `http` or `https`, at most 2000 characters.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtMost(2000),
					stringCheck{description: "value must be an http or https URL", summary: "Invalid URL", check: checkHTTPURL},
				},
			},
			"title":       attachmentTitleAttribute("link", "`Link`, which ImmobilienScout24 sets"),
			"external_id": attachmentExternalIDAttribute(),
		},
	}
}

// defaultTitle is the file name without its extension, trimmed and cut to the
// 30 characters a title may have.
func defaultTitle(file string) string {
	base := filepath.Base(file)
	title := []rune(strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base))))
	if len(title) > maxAttachmentTitle {
		title = []rune(strings.TrimSpace(string(title[:maxAttachmentTitle])))
	}
	return string(title)
}
