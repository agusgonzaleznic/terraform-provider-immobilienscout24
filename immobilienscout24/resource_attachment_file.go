package immobilienscout24

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure      = &fileAttachmentResource{}
	_ resource.ResourceWithImportState    = &fileAttachmentResource{}
	_ resource.ResourceWithModifyPlan     = &fileAttachmentResource{}
	_ resource.ResourceWithValidateConfig = &fileAttachmentResource{}
)

// fileAttachmentResource is immobilienscout24_attachment_picture or
// immobilienscout24_attachment_pdf, depending on its kind.
type fileAttachmentResource struct {
	kind   *fileKind
	client *Client
}

// fileAttachmentModel holds the attributes of a picture or PDF document.
type fileAttachmentModel struct {
	ID           types.String `tfsdk:"id"`
	RealEstateID types.String `tfsdk:"real_estate_id"`
	File         types.String `tfsdk:"file"`
	FileSHA256   types.String `tfsdk:"file_sha256"`
	ContentType  types.String `tfsdk:"content_type"`
	Title        types.String `tfsdk:"title"`
	ExternalID   types.String `tfsdk:"external_id"`
	Floorplan    types.Bool   `tfsdk:"floorplan"`
}

// pictureModel adds the attribute that only a picture has. The resource works
// on it for both kinds; get and set leave TitlePicture out for a PDF document.
type pictureModel struct {
	fileAttachmentModel
	TitlePicture types.Bool `tfsdk:"title_picture"`
}

// NewAttachmentPictureResource returns the immobilienscout24_attachment_picture resource.
func NewAttachmentPictureResource() resource.Resource {
	return &fileAttachmentResource{kind: pictureKind}
}

// NewAttachmentPDFResource returns the immobilienscout24_attachment_pdf resource.
func NewAttachmentPDFResource() resource.Resource {
	return &fileAttachmentResource{kind: pdfKind}
}

func (r *fileAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.kind.typeName
}

func (r *fileAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = fileAttachmentSchema(r.kind)
}

func (r *fileAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	r.client = client
}

// ValidateConfig asks for a content_type when the extension of file does not
// tell it.
func (r *fileAttachmentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var file, contentType types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("file"), &file)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("content_type"), &contentType)...)
	if resp.Diagnostics.HasError() || file.IsNull() || file.IsUnknown() || !contentType.IsNull() {
		return
	}
	if _, ok := r.kind.contentType(file.ValueString()); !ok {
		resp.Diagnostics.AddAttributeError(path.Root("file"), "Unknown file type",
			fmt.Sprintf("The content type of %q does not follow from its extension. Use a file ending in %s, "+
				"or set content_type.", file.ValueString(), r.kind.extensions()))
	}
}

// ModifyPlan reads the file: its SHA-256 is file_sha256, and a new one, like a
// new content type, replaces the attachment, because the API cannot change the
// file of an attachment. A file that is only known after apply replaces it too.
func (r *fileAttachmentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var file, contentType types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("file"), &file)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("content_type"), &contentType)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sum, derived := types.StringUnknown(), types.StringUnknown()
	if !file.IsUnknown() {
		s, err := fileSHA256(file.ValueString())
		if err != nil && !req.State.Raw.IsNull() {
			// The attachment is already uploaded. A missing file must not block
			// a destroy, whose refresh plans the resource too, nor plan a
			// replacement that would delete the attachment and then fail to
			// upload. Keep what was uploaded and warn.
			r.keepUploaded(ctx, req, resp, contentType.IsNull())
			resp.Diagnostics.AddAttributeWarning(path.Root("file"), "Cannot read file",
				fmt.Sprintf("%s. The attachment already uploaded is kept as it is; restore the file to change it.", err))
			return
		}
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("file"), "Cannot read file", err.Error())
			return
		}
		sum = types.StringValue(s)
		if ct, ok := r.kind.contentType(file.ValueString()); ok {
			derived = types.StringValue(ct)
		}
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("file_sha256"), sum)...)
	if contentType.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("content_type"), derived)...)
	}
	if !file.IsUnknown() {
		r.planDefaultTitle(ctx, req, resp, file.ValueString())
	}
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() {
		return
	}

	for _, p := range []path.Path{path.Root("file_sha256"), path.Root("content_type")} {
		var prior, planned types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, p, &prior)...)
		resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, p, &planned)...)
		// An import cannot read the content type, so learning it is no change.
		if p.Equal(path.Root("content_type")) && prior.IsNull() {
			continue
		}
		if !planned.Equal(prior) {
			resp.RequiresReplace = append(resp.RequiresReplace, p)
		}
	}
}

// planDefaultTitle gives a new attachment without a configured title the
// title ImmobilienScout24 would give it, the file name without its extension,
// but sends it in the metadata. The API reads the file name of an upload as
// Latin-1, so "Küche 1.jpg" became "KÃ¼che 1" (observed 2026-09-30), while it
// keeps the UTF-8 of the metadata.
func (r *fileAttachmentResource) planDefaultTitle(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, file string) {
	var configured, planned types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("title"), &configured)...)
	resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("title"), &planned)...)
	if !configured.IsNull() || !planned.IsUnknown() {
		return
	}
	if title := defaultTitle(file); title != "" {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("title"), types.StringValue(title))...)
	}
}

// keepUploaded plans the checksum, and the content type when it is not
// configured, as they are in the state: the attachment stays as uploaded.
func (r *fileAttachmentResource) keepUploaded(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, derivedType bool) {
	attributes := []path.Path{path.Root("file_sha256")}
	if derivedType {
		attributes = append(attributes, path.Root("content_type"))
	}
	for _, p := range attributes {
		var prior types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, p, &prior)...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, p, prior)...)
	}
}

func (r *fileAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pictureModel
	resp.Diagnostics.Append(r.get(ctx, req.Plan, &plan)...)
	makeTitle := r.configuresTitlePicture(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	realEstateID, name := plan.RealEstateID.ValueString(), plan.File.ValueString()

	content, err := readFile(name)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("file"), "Cannot read file", err.Error())
		return
	}
	digest := sha256.Sum256(content)
	sum := hex.EncodeToString(digest[:])
	if !plan.FileSHA256.IsUnknown() && plan.FileSHA256.ValueString() != sum {
		resp.Diagnostics.AddAttributeError(path.Root("file"), "File changed after the plan",
			fmt.Sprintf("%s has other content than when Terraform planned this change. Plan again.", name))
		return
	}
	contentType, ok := plan.ContentType.ValueString(), true
	if plan.ContentType.IsUnknown() {
		contentType, ok = r.kind.contentType(name)
	}
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("file"), "Unknown file type",
			fmt.Sprintf("The content type of %q does not follow from its extension; set content_type.", name))
		return
	}
	if makeTitle && !r.client.claimTitlePicture(realEstateID) {
		addTwoTitlePicturesError(&resp.Diagnostics, realEstateID)
		return
	}

	id, err := r.client.UploadAttachment(ctx, realEstateID, plan.toDocument(r.kind, sum, makeTitle),
		attachmentFile{Name: uploadFileName(name), ContentType: contentType, Content: content})
	if err != nil {
		if makeTitle {
			r.client.releaseTitlePicture(realEstateID)
		}
		addCreateAttachmentError(&resp.Diagnostics, r.kind.noun, realEstateID, err)
		return
	}

	// Record the id before reading back: if the read fails, Terraform keeps
	// the attachment as tainted instead of losing track of it.
	plan.ID, plan.FileSHA256, plan.ContentType = types.StringValue(id), types.StringValue(sum), types.StringValue(contentType)
	if plan.TitlePicture.IsUnknown() {
		plan.TitlePicture = types.BoolNull()
	}
	if plan.Title.IsUnknown() {
		plan.Title = types.StringNull()
	}
	resp.Diagnostics.Append(r.set(ctx, &resp.State, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readInto(ctx, &plan, makeTitle, &resp.State, &resp.Diagnostics, "Error reading "+r.kind.noun+" after uploading it")
}

func (r *fileAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pictureModel
	resp.Diagnostics.Append(r.get(ctx, req.State, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	doc, err := r.client.GetAttachment(ctx, state.RealEstateID.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.kind.noun, err.Error())
		return
	}
	m, err := r.fromDocument(doc, &state)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected attachment from the API", err.Error())
		return
	}
	resp.Diagnostics.Append(r.set(ctx, &resp.State, m)...)
}

func (r *fileAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state pictureModel
	resp.Diagnostics.Append(r.get(ctx, req.Plan, &plan)...)
	resp.Diagnostics.Append(r.get(ctx, req.State, &state)...)
	makeTitle := r.configuresTitlePicture(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	realEstateID, id := state.RealEstateID.ValueString(), state.ID.ValueString()
	plan.ID = state.ID
	if makeTitle && !r.client.claimTitlePicture(realEstateID) {
		addTwoTitlePicturesError(&resp.Diagnostics, realEstateID)
		return
	}

	// PUT replaces the whole metadata and clears a checksum it leaves out
	// (observed 2026-09-30), so it sends the stored one again. The planned one
	// is the same: a new checksum replaces the attachment instead.
	if err := r.client.UpdateAttachment(ctx, realEstateID, id, plan.toDocument(r.kind, state.FileSHA256.ValueString(), makeTitle)); err != nil {
		if makeTitle {
			r.client.releaseTitlePicture(realEstateID)
		}
		resp.Diagnostics.AddError("Error updating "+r.kind.noun, err.Error())
		return
	}
	r.readInto(ctx, &plan, makeTitle, &resp.State, &resp.Diagnostics, "Error reading "+r.kind.noun+" after updating it")
}

func (r *fileAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pictureModel
	resp.Diagnostics.Append(r.get(ctx, req.State, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteAttachment(ctx, r.client, state.RealEstateID.ValueString(), state.ID.ValueString(), r.kind.noun, &resp.Diagnostics)
}

func (r *fileAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importAttachment(ctx, req, resp)
}

// get reads a plan or state into m; a PDF document has no title_picture.
func (r *fileAttachmentResource) get(ctx context.Context, from interface {
	Get(context.Context, any) diag.Diagnostics
}, m *pictureModel) diag.Diagnostics {
	if r.kind.titlePicture {
		return from.Get(ctx, m)
	}
	return from.Get(ctx, &m.fileAttachmentModel)
}

// set stores m; a PDF document has no title_picture.
func (r *fileAttachmentResource) set(ctx context.Context, state *tfsdk.State, m *pictureModel) diag.Diagnostics {
	if r.kind.titlePicture {
		return state.Set(ctx, m)
	}
	return state.Set(ctx, &m.fileAttachmentModel)
}

// configuresTitlePicture reports whether the configuration sets title_picture = true.
func (r *fileAttachmentResource) configuresTitlePicture(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) bool {
	if !r.kind.titlePicture {
		return false
	}
	var v types.Bool
	diags.Append(config.GetAttribute(ctx, path.Root("title_picture"), &v)...)
	return v.ValueBool()
}

// readInto reads the attachment back after a write and stores it. prior holds
// what the API does not return, the file and its content type. If the write
// made it the title picture but the API reports that it is not, another
// picture took the flag in the meantime. That is reported here rather than
// left to Terraform's generic "inconsistent result" error.
func (r *fileAttachmentResource) readInto(ctx context.Context, prior *pictureModel, madeTitle bool, state *tfsdk.State, diags *diag.Diagnostics, summary string) {
	realEstateID, id := prior.RealEstateID.ValueString(), prior.ID.ValueString()
	doc, err := r.client.GetAttachment(ctx, realEstateID, id)
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	m, err := r.fromDocument(doc, prior)
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	diags.Append(r.set(ctx, state, m)...)
	if madeTitle && !m.TitlePicture.ValueBool() {
		diags.AddAttributeError(path.Root("title_picture"), "Another picture became the title picture",
			fmt.Sprintf("The configuration sets title_picture = true, but right after the write ImmobilienScout24 "+
				"reports that picture %s is not the title picture of real estate %s, so another picture took the "+
				"flag in the meantime. A listing has exactly one title picture: set title_picture = true on one "+
				"immobilienscout24_attachment_picture per listing only.", id, realEstateID))
	}
}

// fromDocument maps a GET response onto the model. The API returns the
// metadata as it was sent (observed 2026-09-30), and neither the file nor
// its content type, which are taken from prior. Without the checksum the
// provider stored, file_sha256 is null or another value, and the next plan
// replaces the attachment.
func (r *fileAttachmentResource) fromDocument(doc *attachmentDocument, prior *pictureModel) (*pictureModel, error) {
	if doc.Type != r.kind.xsiType {
		return nil, wrongAttachmentType(doc.Type, prior.RealEstateID.ValueString(), prior.ID.ValueString(), r.kind.xsiType)
	}
	m := &pictureModel{fileAttachmentModel: fileAttachmentModel{
		ID:           prior.ID,
		RealEstateID: prior.RealEstateID,
		File:         prior.File,
		FileSHA256:   optionalString(doc.ExternalCheckSum),
		ContentType:  prior.ContentType,
		Title:        optionalString(doc.Title),
		ExternalID:   optionalString(doc.ExternalID),
		Floorplan:    types.BoolPointerValue(doc.Floorplan),
	}}
	if r.kind.titlePicture {
		m.TitlePicture = types.BoolPointerValue(doc.TitlePicture)
	}
	return m, nil
}

// toDocument builds the complete metadata document from a plan, with checksum
// as externalCheckSum. A picture always carries titlePicture, which the
// schema requires: true when makeTitle is set, false otherwise, which the API
// ignores on the title picture.
func (m *pictureModel) toDocument(kind *fileKind, checksum string, makeTitle bool) *attachmentDocument {
	f := attachmentFields{
		Title:            m.Title.ValueString(),
		ExternalID:       m.ExternalID.ValueString(),
		ExternalCheckSum: checksum,
		Floorplan:        m.Floorplan.ValueBoolPointer(),
	}
	if kind.titlePicture {
		f.TitlePicture = &makeTitle
	}
	return &attachmentDocument{Type: kind.xsiType, attachmentFields: f}
}

func addTwoTitlePicturesError(diags *diag.Diagnostics, realEstateID string) {
	diags.AddAttributeError(path.Root("title_picture"), "Two pictures set title_picture = true",
		fmt.Sprintf("Another immobilienscout24_attachment_picture of real estate %s sets title_picture = true and "+
			"was written in this apply. A listing has exactly one title picture: set title_picture = true on one "+
			"picture per listing only.", realEstateID))
}

func addCreateAttachmentError(diags *diag.Diagnostics, noun, realEstateID string, err error) {
	if errors.Is(err, ErrNotFound) {
		diags.AddError("Real estate not found",
			fmt.Sprintf("Real estate %s does not exist in the account of the access token, so the %s cannot be "+
				"attached to it.\n\n%s", realEstateID, noun, err))
		return
	}
	diags.AddError("Error creating "+noun, err.Error())
}

// deleteAttachment deletes an attachment. One that is already gone, for
// example because its listing was deleted, answers 404, which counts as done.
func deleteAttachment(ctx context.Context, client *Client, realEstateID, id, noun string, diags *diag.Diagnostics) {
	err := client.DeleteAttachment(ctx, realEstateID, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		diags.AddError("Error deleting "+noun, err.Error())
	}
}

// importAttachment takes the import id {real_estate_id}/{attachment_id}.
func importAttachment(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	realEstateID, id, found := strings.Cut(req.ID, "/")
	if !found || !positiveID.MatchString(realEstateID) || !positiveID.MatchString(id) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Import an attachment by {real_estate_id}/{attachment_id}, for example 315000001/904864036, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("real_estate_id"), realEstateID)...)
}

// attachmentResources names the resource that manages each attachment type.
var attachmentResources = map[string]string{
	attachmentPicture: "immobilienscout24_attachment_picture",
	attachmentPDF:     "immobilienscout24_attachment_pdf",
	attachmentLink:    "immobilienscout24_attachment_link",
}

// wrongAttachmentType reports an attachment of another type than expected,
// which happens when an id is imported into the wrong resource. got comes
// from the response, so it goes through errorText.
func wrongAttachmentType(got, realEstateID, id, want string) error {
	if got == "" {
		return fmt.Errorf("attachment %s of real estate %s has no xsi:type, expected common:%s", id, realEstateID, want)
	}
	msg := fmt.Sprintf("attachment %s of real estate %s is a common:%s, not a common:%s", id, realEstateID, errorText(got), want)
	if name, ok := attachmentResources[got]; ok {
		return fmt.Errorf("%s; import it as %s instead", msg, name)
	}
	return fmt.Errorf("%s, which this provider does not manage", msg)
}
