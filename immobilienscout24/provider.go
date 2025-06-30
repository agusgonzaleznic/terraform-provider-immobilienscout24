package immobilienscout24

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type immobilienscout24Provider struct{}

type immobilienscout24ProviderModel struct {
	ConsumerKey       types.String `tfsdk:"consumer_key"`
	ConsumerSecret    types.String `tfsdk:"consumer_secret"`
	AccessToken       types.String `tfsdk:"access_token"`
	AccessTokenSecret types.String `tfsdk:"access_token_secret"`
}

func New() provider.Provider {
	return &immobilienscout24Provider{}
}

func (p *immobilienscout24Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "immobilienscout24"
}

func (p *immobilienscout24Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"consumer_key": schema.StringAttribute{
				Required: true,
			},
			"consumer_secret": schema.StringAttribute{
				Required: true,
			},
			"access_token": schema.StringAttribute{
				Required: true,
			},
			"access_token_secret": schema.StringAttribute{
				Required: true,
			},
		},
	}
}

func (p *immobilienscout24Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data immobilienscout24ProviderModel
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := NewClient(
		data.ConsumerKey.ValueString(),
		data.ConsumerSecret.ValueString(),
		data.AccessToken.ValueString(),
		data.AccessTokenSecret.ValueString(),
	)

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *immobilienscout24Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRealEstateResource,
	}
}

func (p *immobilienscout24Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
