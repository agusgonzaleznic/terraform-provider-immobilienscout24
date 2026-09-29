package immobilienscout24

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure      = &contactResource{}
	_ resource.ResourceWithImportState    = &contactResource{}
	_ resource.ResourceWithValidateConfig = &contactResource{}
)

type contactResource struct {
	client *Client
}

// NewContactResource returns the immobilienscout24_contact resource.
func NewContactResource() resource.Resource {
	return &contactResource{}
}

func (r *contactResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (r *contactResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = contactSchema()
}

func (r *contactResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig rejects an address without any attribute. Whether the API
// keeps an empty address is not documented, and if it dropped one the result
// would not match the configuration.
func (r *contactResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var address types.Object
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("address"), &address)...)
	if resp.Diagnostics.HasError() || address.IsNull() || address.IsUnknown() {
		return
	}
	for _, v := range address.Attributes() {
		if !v.IsNull() {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(path.Root("address"), "Empty address",
		"Set at least one of street, house_number, postcode and city, or leave address out.")
}

func (r *contactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	makeDefault := configuresDefault(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateContact(ctx, plan.toDocument(makeDefault))
	if err != nil {
		resp.Diagnostics.AddError("Error creating contact", err.Error())
		return
	}

	// Record the id before reading back: if the read fails, Terraform keeps
	// the contact as tainted instead of losing track of it.
	plan.ID = types.StringValue(id)
	if plan.DefaultContact.IsUnknown() {
		plan.DefaultContact = types.BoolNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readInto(ctx, id, makeDefault, &resp.State, &resp.Diagnostics, "Error reading contact after creating it")
}

func (r *contactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state contactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	doc, err := r.client.GetContact(ctx, state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading contact", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, contactFromDocument(state.ID.ValueString(), doc))...)
}

func (r *contactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	makeDefault := configuresDefault(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	// PUT is a full replacement (observed 2026-09-29), so send the whole
	// document from the plan.
	if err := r.client.UpdateContact(ctx, id, plan.toDocument(makeDefault)); err != nil {
		resp.Diagnostics.AddError("Error updating contact", err.Error())
		return
	}
	r.readInto(ctx, id, makeDefault, &resp.State, &resp.Diagnostics, "Error reading contact after updating it")
}

func (r *contactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state contactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	err := r.client.DeleteContact(ctx, id)
	// Terraform may delete the old default contact while another contact
	// takes the default in the same apply, so the refusal can clear within
	// seconds. Try again a few times before giving up.
	for _, wait := range defaultContactDeleteWait {
		if !errors.Is(err, ErrDefaultContact) {
			break
		}
		select {
		case <-ctx.Done():
			resp.Diagnostics.AddError("Error deleting contact", ctx.Err().Error())
			return
		case <-time.After(wait):
		}
		err = r.client.DeleteContact(ctx, id)
	}
	switch {
	case errors.Is(err, ErrDefaultContact):
		resp.Diagnostics.AddError("Cannot delete the default contact",
			fmt.Sprintf("Contact %s is the account's default contact, which ImmobilienScout24 refuses to delete. "+
				"Make another contact the default (set default_contact = true on another immobilienscout24_contact, "+
				"or choose another default contact on the website), then destroy again.\n\n%s", id, err))
	case err != nil && !errors.Is(err, ErrNotFound):
		resp.Diagnostics.AddError("Error deleting contact", err.Error())
	}
}

// defaultContactDeleteWait is how long Delete waits between tries while the
// API refuses to delete the default contact, 15 seconds in all. Tests
// shorten it.
var defaultContactDeleteWait = []time.Duration{
	time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second, 5 * time.Second,
}

func (r *contactResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !positiveID.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Import a contact by its numeric ImmobilienScout24 id, got %q.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// configuresDefault reports whether the configuration sets default_contact = true.
func configuresDefault(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) bool {
	var v types.Bool
	diags.Append(config.GetAttribute(ctx, path.Root("default_contact"), &v)...)
	return v.ValueBool()
}

// readInto reads the contact back after a write and stores it. If the write
// made it the default contact but the API reports that it is not, another
// contact took the flag in the meantime. That is reported here rather than
// left to Terraform's generic "inconsistent result" error.
func (r *contactResource) readInto(ctx context.Context, id string, madeDefault bool, state *tfsdk.State, diags *diag.Diagnostics, summary string) {
	doc, err := r.client.GetContact(ctx, id)
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	m := contactFromDocument(id, doc)
	diags.Append(state.Set(ctx, m)...)
	if madeDefault && !m.DefaultContact.ValueBool() {
		diags.AddAttributeError(path.Root("default_contact"), "Another contact became the default contact",
			fmt.Sprintf("The configuration sets default_contact = true, but right after the write ImmobilienScout24 "+
				"reports that contact %s is not the account's default contact, so another contact took the flag in the "+
				"meantime. An account has exactly one default contact: set default_contact = true on one "+
				"immobilienscout24_contact only.", id))
	}
}
