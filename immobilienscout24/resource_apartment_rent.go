package immobilienscout24

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure      = &apartmentRentResource{}
	_ resource.ResourceWithImportState    = &apartmentRentResource{}
	_ resource.ResourceWithValidateConfig = &apartmentRentResource{}
)

type apartmentRentResource struct {
	client *Client
}

// NewApartmentRentResource returns the immobilienscout24_apartment_rent resource.
func NewApartmentRentResource() resource.Resource {
	return &apartmentRentResource{}
}

func (r *apartmentRentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_apartment_rent"
}

func (r *apartmentRentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = apartmentRentSchema()
}

func (r *apartmentRentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig enforces the documented cross-field rules the schema cannot express.
func (r *apartmentRentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var hasCourtage, courtage, heatingIncluded types.String
	var heatingCosts types.Float64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("courtage").AtName("has_courtage"), &hasCourtage)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("courtage").AtName("courtage"), &courtage)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("heating_costs"), &heatingCosts)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("heating_costs_in_service_charge"), &heatingIncluded)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Field table: courtage is "Only mandatory, if hasCourtage=true", and the
	// enum has YES rather than true.
	if hasCourtage.ValueString() == "YES" && courtage.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("courtage").AtName("courtage"), "Missing commission",
			"courtage.courtage is required when courtage.has_courtage is \"YES\".")
	}
	// Field table: "If you've entered a value for heating costs, than
	// NOT_APPLICABLE is not allowed for this attribute."
	if !heatingCosts.IsNull() && heatingIncluded.ValueString() == "NOT_APPLICABLE" {
		resp.Diagnostics.AddAttributeError(path.Root("heating_costs_in_service_charge"), "Invalid combination",
			"heating_costs_in_service_charge cannot be \"NOT_APPLICABLE\" when heating_costs is set.")
	}
}

func (r *apartmentRentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apartmentRentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateApartmentRent(ctx, plan.toDocument())
	if err != nil {
		resp.Diagnostics.AddError("Error creating apartment rent", err.Error())
		return
	}

	// Record the id before reading back: if the read fails, Terraform keeps
	// the object as tainted instead of losing track of it.
	plan.ID = types.StringValue(id)
	if plan.ExternalID.IsUnknown() {
		plan.ExternalID = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readInto(ctx, id, &plan, &resp.State, &resp.Diagnostics, "Error reading apartment rent after creating it")
}

func (r *apartmentRentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apartmentRentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	doc, err := r.client.GetApartmentRent(ctx, state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading apartment rent", err.Error())
		return
	}
	r.setFromDocument(ctx, state.ID.ValueString(), doc, &state, &resp.State, &resp.Diagnostics)
}

func (r *apartmentRentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state apartmentRentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	plan.ID = state.ID

	// PUT is a full replacement, so send the whole document from the plan.
	if err := r.client.UpdateApartmentRent(ctx, id, plan.toDocument()); err != nil {
		resp.Diagnostics.AddError("Error updating apartment rent", err.Error())
		return
	}
	r.readInto(ctx, id, &plan, &resp.State, &resp.Diagnostics, "Error reading apartment rent after updating it")
}

func (r *apartmentRentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apartmentRentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteRealEstate(ctx, state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Error deleting apartment rent", err.Error())
	}
}

func (r *apartmentRentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !isDigits(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Import an apartment by its numeric ImmobilienScout24 id (the scout id), got %q.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readInto reads the object back after a write and stores it, compared
// against the plan so that server-side number formatting causes no diff.
func (r *apartmentRentResource) readInto(ctx context.Context, id string, plan *apartmentRentModel, state *tfsdk.State, diags *diag.Diagnostics, summary string) {
	doc, err := r.client.GetApartmentRent(ctx, id)
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	r.setFromDocument(ctx, id, doc, plan, state, diags)
}

func (r *apartmentRentResource) setFromDocument(ctx context.Context, id string, doc *apartmentRentDocument, prior *apartmentRentModel, state *tfsdk.State, diags *diag.Diagnostics) {
	m, err := fromDocument(id, doc, prior)
	if err != nil {
		diags.AddError("Unexpected apartment rent from the API", err.Error())
		return
	}
	diags.Append(state.Set(ctx, m)...)
}

// isDigits reports whether s is a non-empty string of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
