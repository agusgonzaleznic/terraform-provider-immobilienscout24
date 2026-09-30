package immobilienscout24

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure   = &publicationResource{}
	_ resource.ResourceWithImportState = &publicationResource{}
)

// positiveID matches the ids a publication is made of. A leading zero is
// rejected: the API reads the ids as numbers, so the publication it creates
// would not have the id the provider expects.
var positiveID = regexp.MustCompile(`^[1-9][0-9]*$`)

type publicationResource struct {
	client *Client
}

type publicationModel struct {
	ID           types.String `tfsdk:"id"`
	RealEstateID types.String `tfsdk:"real_estate_id"`
	ChannelID    types.String `tfsdk:"channel_id"`
}

// NewPublicationResource returns the immobilienscout24_publication resource.
func NewPublicationResource() resource.Resource {
	return &publicationResource{}
}

func (r *publicationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_publication"
}

func (r *publicationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	idValidators := []validator.String{
		stringvalidator.RegexMatches(positiveID, "must be a whole number in digits, without a leading zero"),
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Publishes a real estate listing on one publish channel (`POST /offer/v1.0/publish`). " +
			"Listings that this provider creates start unpublished. Each channel a listing is published on is a " +
			"resource of its own, and destroying it unpublishes the listing on that channel only. Changing either " +
			"argument unpublishes the listing and publishes it anew.\n\n" +
			"**Publishing on channel `10000` in production uses the paid contingent of the account.** Without " +
			"contingent the API answers `No contingent available`.\n\n" +
			"Unpublishing a listing from channel `10000` deactivates it on ImmobilienScout24, even while it stays " +
			"published on channel `10001`.\n\n" +
			"The API documentation asks to send publish requests one after the other, so the provider sends them " +
			"one at a time, also when Terraform creates several publications in parallel. That holds within one " +
			"provider configuration only: provider aliases, separate runs and parallel CI jobs are not coordinated.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The publication id, `{real_estate_id}_{channel_id}`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"real_estate_id": schema.StringAttribute{
				MarkdownDescription: "The scout object id of the listing: the `id` of any listing resource, for " +
					"example `immobilienscout24_apartment_rent.example.id`. Changing it forces a new publication.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    idValidators,
			},
			"channel_id": schema.StringAttribute{
				MarkdownDescription: "The publish channel: `10000` for ImmobilienScout24 (www.immobilienscout24.de) " +
					"or `10001` for the realtor's own homepage. Publishing on `10000` in production uses paid " +
					"contingent. Changing it forces a new publication.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    idValidators,
			},
		},
	}
}

func (r *publicationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *publicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	realEstateID, channelID := plan.RealEstateID.ValueString(), plan.ChannelID.ValueString()

	id, err := r.client.Publish(ctx, realEstateID, channelID)
	switch {
	case errors.Is(err, ErrConflict):
		// The API also answers 409 to concurrent access, so only suggest an
		// import when the publication really exists.
		if _, gerr := r.client.GetPublication(ctx, publicationID(realEstateID, channelID)); gerr != nil {
			resp.Diagnostics.AddError("Publish request conflicted",
				fmt.Sprintf("The API refused to publish real estate %s on channel %s with a conflict, and no such "+
					"publication exists. Retry the apply.\n\n%s", realEstateID, channelID, err))
			return
		}
		resp.Diagnostics.AddError("Listing is already published on this channel",
			fmt.Sprintf("Real estate %s is already published on channel %s. To manage that publication with "+
				"Terraform, import it:\n\n  terraform import immobilienscout24_publication.<name> %s\n\n%s",
				realEstateID, channelID, publicationID(realEstateID, channelID), err))
		return
	case errors.Is(err, ErrNotFound):
		resp.Diagnostics.AddError("Real estate not found",
			fmt.Sprintf("Real estate %s does not exist in the account of the access token, so it cannot be "+
				"published.\n\n%s", realEstateID, err))
		return
	case err != nil:
		resp.Diagnostics.AddError("Error publishing real estate", err.Error())
		return
	}

	// Record the id before reading back: if the read fails, Terraform keeps
	// the publication as tainted instead of losing track of it.
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p, err := r.client.GetPublication(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading publication after creating it", err.Error())
		return
	}
	setPublication(ctx, p, &resp.State, &resp.Diagnostics)
}

func (r *publicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p, err := r.client.GetPublication(ctx, state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading publication", err.Error())
		return
	}
	setPublication(ctx, p, &resp.State, &resp.Diagnostics)
}

// Update is never called with a change: every argument forces replacement.
func (r *publicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan publicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A publication that is already gone, for example because its real estate
	// was deleted, answers 404.
	err := r.client.Unpublish(ctx, state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Error unpublishing real estate", err.Error())
	}
}

func (r *publicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	realEstateID, channelID, ok := parsePublicationID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Import a publication by {real_estate_id}_{channel_id}, for example 315000001_10000, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("real_estate_id"), realEstateID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("channel_id"), channelID)...)
}

// parsePublicationID splits a publication id, "{real_estate_id}_{channel_id}".
func parsePublicationID(id string) (realEstateID, channelID string, ok bool) {
	realEstateID, channelID, found := strings.Cut(id, "_")
	if !found || !positiveID.MatchString(realEstateID) || !positiveID.MatchString(channelID) {
		return "", "", false
	}
	return realEstateID, channelID, true
}

func setPublication(ctx context.Context, p *Publication, state *tfsdk.State, diags *diag.Diagnostics) {
	diags.Append(state.Set(ctx, &publicationModel{
		ID:           types.StringValue(p.ID),
		RealEstateID: types.StringValue(p.RealEstateID),
		ChannelID:    types.StringValue(p.ChannelID),
	})...)
}
