package immobilienscout24

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure      = &listingResource[apartmentRentModel, apartmentRentDocument]{}
	_ resource.ResourceWithImportState    = &listingResource[apartmentRentModel, apartmentRentDocument]{}
	_ resource.ResourceWithValidateConfig = &listingResource[apartmentRentModel, apartmentRentDocument]{}
)

// listingKind describes one listing resource type for the CRUD flow that all
// of them share. M is the type's model, which embeds listingModel, and D its
// document, which embeds listingFields.
type listingKind[M, D any] struct {
	typeName string // the resource type name after the provider's, such as "_apartment_rent"
	realEstateType
	noun   string // the resource in messages, such as "apartment for rent"
	schema func() schema.Schema
	// listing returns the attributes that every listing type has.
	listing    func(*M) *listingModel
	toDocument func(*M) *D
	toModel    func(doc *D, id string, prior *M) (*M, error)
	// validate adds the type's own rules to ValidateConfig; nil if it has none.
	validate func(context.Context, tfsdk.Config, *diag.Diagnostics)
}

// listingResource is a listing resource, such as
// immobilienscout24_apartment_rent, depending on its kind.
type listingResource[M, D any] struct {
	kind   *listingKind[M, D]
	client *Client
}

func (r *listingResource[M, D]) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.kind.typeName
}

func (r *listingResource[M, D]) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = r.kind.schema()
}

func (r *listingResource[M, D]) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig enforces the documented and observed cross-field rules the
// schema cannot express.
func (r *listingResource[M, D]) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	validateCourtage(ctx, req.Config, &resp.Diagnostics)
	validateEnergy(ctx, req.Config, &resp.Diagnostics)
	if r.kind.validate != nil {
		r.kind.validate(ctx, req.Config, &resp.Diagnostics)
	}
}

func (r *listingResource[M, D]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateRealEstate(ctx, &r.kind.realEstateType, r.kind.toDocument(&plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating "+r.kind.noun, err.Error())
		return
	}

	// Record the id before reading back: if the read fails, Terraform keeps
	// the object as tainted instead of losing track of it.
	l := r.kind.listing(&plan)
	l.ID = types.StringValue(id)
	if l.ExternalID.IsUnknown() {
		l.ExternalID = types.StringNull()
	}
	if l.ContactID.IsUnknown() {
		l.ContactID = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readInto(ctx, id, &plan, &resp.State, &resp.Diagnostics, "Error reading "+r.kind.noun+" after creating it")
}

func (r *listingResource[M, D]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state M
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := r.kind.listing(&state).ID.ValueString()

	doc := new(D)
	err := r.client.GetRealEstate(ctx, &r.kind.realEstateType, id, doc)
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.kind.noun, err.Error())
		return
	}
	r.setFromDocument(ctx, id, doc, &state, &resp.State, &resp.Diagnostics)
}

func (r *listingResource[M, D]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	l := r.kind.listing(&plan)
	l.ID = r.kind.listing(&state).ID
	id := l.ID.ValueString()

	// PUT is a full replacement, so send the whole document from the plan.
	// A PUT without a contact would reset the listing to the default contact,
	// so when the configuration leaves contact_id out, send the contact the
	// listing has right now. The state can be out of date: in the same apply
	// Terraform may already have deleted that contact, which moves the listing
	// to the default contact, and a PUT naming it fails with 412.
	if l.ContactID.IsUnknown() || l.ContactID.IsNull() {
		var current listingFields
		if err := r.client.GetRealEstate(ctx, &r.kind.realEstateType, id, &current); err != nil {
			resp.Diagnostics.AddError("Error reading the listing's contact before updating it", err.Error())
			return
		}
		l.ContactID = types.StringNull()
		if current.Contact != nil {
			l.ContactID = optionalString(strings.TrimSpace(current.Contact.ID))
		}
	}
	if err := r.client.UpdateRealEstate(ctx, &r.kind.realEstateType, id, r.kind.toDocument(&plan)); err != nil {
		resp.Diagnostics.AddError("Error updating "+r.kind.noun, err.Error())
		return
	}
	r.readInto(ctx, id, &plan, &resp.State, &resp.Diagnostics, "Error reading "+r.kind.noun+" after updating it")
}

func (r *listingResource[M, D]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state M
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteRealEstate(ctx, r.kind.listing(&state).ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Error deleting "+r.kind.noun, err.Error())
	}
}

func (r *listingResource[M, D]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !isDigits(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Import a listing by its numeric ImmobilienScout24 id (the scout id), got %q.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readInto reads the object back after a write and stores it, compared
// against the plan so that server-side number formatting causes no diff.
func (r *listingResource[M, D]) readInto(ctx context.Context, id string, plan *M, state *tfsdk.State, diags *diag.Diagnostics, summary string) {
	doc := new(D)
	if err := r.client.GetRealEstate(ctx, &r.kind.realEstateType, id, doc); err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	r.setFromDocument(ctx, id, doc, plan, state, diags)
}

func (r *listingResource[M, D]) setFromDocument(ctx context.Context, id string, doc *D, prior *M, state *tfsdk.State, diags *diag.Diagnostics) {
	m, err := r.kind.toModel(doc, id, prior)
	if err != nil {
		diags.AddError("Unexpected "+r.kind.noun+" from the API", err.Error())
		return
	}
	diags.Append(state.Set(ctx, m)...)
}

// validateCourtage enforces the documented rule of the commission.
func validateCourtage(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) {
	var hasCourtage, courtage types.String
	var d diag.Diagnostics
	d.Append(config.GetAttribute(ctx, path.Root("courtage").AtName("has_courtage"), &hasCourtage)...)
	d.Append(config.GetAttribute(ctx, path.Root("courtage").AtName("courtage"), &courtage)...)
	diags.Append(d...)
	if d.HasError() {
		return
	}
	// Field table: courtage is "Only mandatory, if hasCourtage=true", and the
	// enum has YES rather than true.
	if hasCourtage.ValueString() == "YES" && courtage.IsNull() {
		diags.AddAttributeError(path.Root("courtage").AtName("courtage"), "Missing commission",
			"courtage.courtage is required when courtage.has_courtage is \"YES\".")
	}
}

// validateHeatingCosts enforces the documented rule of the heating costs, for
// the listing types for rent.
func validateHeatingCosts(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) {
	var heatingIncluded types.String
	var heatingCosts types.Float64
	var d diag.Diagnostics
	d.Append(config.GetAttribute(ctx, path.Root("heating_costs"), &heatingCosts)...)
	d.Append(config.GetAttribute(ctx, path.Root("heating_costs_in_service_charge"), &heatingIncluded)...)
	diags.Append(d...)
	if d.HasError() {
		return
	}
	// Field table: "If you've entered a value for heating costs, than
	// NOT_APPLICABLE is not allowed for this attribute."
	// The attribute defaults to NOT_APPLICABLE, so leaving it out counts too.
	if !heatingCosts.IsNull() && !heatingIncluded.IsUnknown() &&
		(heatingIncluded.IsNull() || heatingIncluded.ValueString() == "NOT_APPLICABLE") {
		diags.AddAttributeError(path.Root("heating_costs_in_service_charge"), "Invalid combination",
			"Set heating_costs_in_service_charge to \"YES\" or \"NO\" when heating_costs is set.")
	}
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
