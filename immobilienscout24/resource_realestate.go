package immobilienscout24

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type realEstateResource struct {
	client *Client
}

func NewRealEstateResource() resource.Resource {
	return &realEstateResource{}
}

type realEstateResourceModel struct {
	Id              types.String `tfsdk:"id"`
	Title           types.String `tfsdk:"title"`
	DescriptionNote types.String `tfsdk:"description_note"`
	Street          types.String `tfsdk:"street"`
	HouseNumber     types.String `tfsdk:"house_number"`
	Postcode        types.String `tfsdk:"postcode"`
	City            types.String `tfsdk:"city"`
}

func (r *realEstateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "immobilienscout24_realestate"
}

func (r *realEstateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"title": schema.StringAttribute{
				Required: true,
			},
			"description_note": schema.StringAttribute{
				Required: true,
			},
			"street": schema.StringAttribute{
				Required: true,
			},
			"house_number": schema.StringAttribute{
				Required: true,
			},
			"postcode": schema.StringAttribute{
				Required: true,
			},
			"city": schema.StringAttribute{
				Required: true,
			},
		},
	}
}

// Wire up the client from the provider
func (r *realEstateResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

// Insert a new real estate object
func (r *realEstateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data realEstateResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	xmlBody := `<realEstate>
    <title>` + xmlEscape(data.Title.ValueString()) + `</title>
    <descriptionNote>` + xmlEscape(data.DescriptionNote.ValueString()) + `</descriptionNote>
    <address>
        <street>` + xmlEscape(data.Street.ValueString()) + `</street>
        <houseNumber>` + xmlEscape(data.HouseNumber.ValueString()) + `</houseNumber>
        <postcode>` + xmlEscape(data.Postcode.ValueString()) + `</postcode>
        <city>` + xmlEscape(data.City.ValueString()) + `</city>
    </address>
</realEstate>`

	apiURL := "https://rest.sandbox-immobilienscout24.de/restapi/api/offer/v1.0/user/me/realestate"
	httpReq, err := http.NewRequest("POST", apiURL, strings.NewReader(xmlBody))
	if err != nil {
		resp.Diagnostics.AddError("Request Error", err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/xml")

	httpResp, err := r.client.httpClient.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("API Error", err.Error())
		return
	}
	defer httpResp.Body.Close()

	respBody, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode != 201 {
		resp.Diagnostics.AddError("API Error", "Status: "+httpResp.Status+"\n"+string(respBody))
		return
	}

	// Parse ID from response XML (format: <realEstate id="1234567">...</realEstate>)
	var res struct {
		XMLName xml.Name `xml:"realEstate"`
		ID      string   `xml:"id,attr"`
	}
	if err := xml.Unmarshal(respBody, &res); err != nil || res.ID == "" {
		resp.Diagnostics.AddError("Response Parsing Error", "Could not parse real estate ID from response: "+err.Error()+"\nBody:\n"+string(respBody))
		return
	}

	data.Id = types.StringValue(res.ID)
	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Stub: Not implemented for MVP
func (r *realEstateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}
func (r *realEstateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}
func (r *realEstateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}

// Helper: Escape XML special characters
func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
