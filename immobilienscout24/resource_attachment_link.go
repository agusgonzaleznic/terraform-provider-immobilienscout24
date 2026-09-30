package immobilienscout24

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure   = &linkResource{}
	_ resource.ResourceWithImportState = &linkResource{}
)

type linkResource struct {
	client *Client
}

type linkModel struct {
	ID           types.String `tfsdk:"id"`
	RealEstateID types.String `tfsdk:"real_estate_id"`
	URL          types.String `tfsdk:"url"`
	Title        types.String `tfsdk:"title"`
	ExternalID   types.String `tfsdk:"external_id"`
}

// NewAttachmentLinkResource returns the immobilienscout24_attachment_link resource.
func NewAttachmentLinkResource() resource.Resource {
	return &linkResource{}
}

func (r *linkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_attachment_link"
}

func (r *linkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = linkSchema()
}

func (r *linkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *linkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan linkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	realEstateID := plan.RealEstateID.ValueString()

	id, err := r.client.CreateAttachment(ctx, realEstateID, plan.toDocument())
	if err != nil {
		addCreateAttachmentError(&resp.Diagnostics, "link", realEstateID, err)
		return
	}

	// Record the id before reading back: if the read fails, Terraform keeps
	// the link as tainted instead of losing track of it.
	plan.ID = types.StringValue(id)
	if plan.Title.IsUnknown() {
		plan.Title = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.readInto(ctx, &plan, &resp.State, &resp.Diagnostics, "Error reading link after creating it")
}

func (r *linkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state linkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	doc, err := r.client.GetAttachment(ctx, state.RealEstateID.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading link", err.Error())
		return
	}
	m, err := linkFromDocument(doc, &state)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected attachment from the API", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *linkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state linkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID

	// PUT is a full replacement (observed 2026-09-30), so send the whole
	// document from the plan.
	if err := r.client.UpdateAttachment(ctx, state.RealEstateID.ValueString(), state.ID.ValueString(), plan.toDocument()); err != nil {
		resp.Diagnostics.AddError("Error updating link", err.Error())
		return
	}
	r.readInto(ctx, &plan, &resp.State, &resp.Diagnostics, "Error reading link after updating it")
}

func (r *linkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state linkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteAttachment(ctx, r.client, state.RealEstateID.ValueString(), state.ID.ValueString(), "link", &resp.Diagnostics)
}

func (r *linkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importAttachment(ctx, req, resp)
}

// readInto reads the link back after a write and stores it.
func (r *linkResource) readInto(ctx context.Context, prior *linkModel, state *tfsdk.State, diags *diag.Diagnostics, summary string) {
	doc, err := r.client.GetAttachment(ctx, prior.RealEstateID.ValueString(), prior.ID.ValueString())
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	m, err := linkFromDocument(doc, prior)
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	diags.Append(state.Set(ctx, m)...)
}

// toDocument builds the complete document from a plan. Null optional values
// are left out, which on PUT clears them.
func (m *linkModel) toDocument() *attachmentDocument {
	return &attachmentDocument{Type: attachmentLink, attachmentFields: attachmentFields{
		Title:      m.Title.ValueString(),
		ExternalID: m.ExternalID.ValueString(),
		URL:        m.URL.ValueString(),
	}}
}

// linkFromDocument maps a GET response onto the model; the ids come from prior.
func linkFromDocument(doc *attachmentDocument, prior *linkModel) (*linkModel, error) {
	if doc.Type != attachmentLink {
		return nil, wrongAttachmentType(doc.Type, prior.RealEstateID.ValueString(), prior.ID.ValueString(), attachmentLink)
	}
	return &linkModel{
		ID:           prior.ID,
		RealEstateID: prior.RealEstateID,
		URL:          types.StringValue(doc.URL),
		Title:        optionalString(doc.Title),
		ExternalID:   optionalString(doc.ExternalID),
	}, nil
}
