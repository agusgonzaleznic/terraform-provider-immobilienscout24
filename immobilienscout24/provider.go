package immobilienscout24

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	environmentSandbox    = "sandbox"
	environmentProduction = "production"
)

// credentialEnv maps each credential attribute to its environment variable.
var credentialEnv = []struct {
	attribute string
	env       string
}{
	{"consumer_key", "IMMOBILIENSCOUT24_CONSUMER_KEY"},
	{"consumer_secret", "IMMOBILIENSCOUT24_CONSUMER_SECRET"},
	{"access_token", "IMMOBILIENSCOUT24_ACCESS_TOKEN"},
	{"access_token_secret", "IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET"},
}

type immobilienscout24Provider struct {
	version string
}

type immobilienscout24ProviderModel struct {
	Environment       types.String `tfsdk:"environment"`
	BaseURL           types.String `tfsdk:"base_url"`
	ConsumerKey       types.String `tfsdk:"consumer_key"`
	ConsumerSecret    types.String `tfsdk:"consumer_secret"`
	AccessToken       types.String `tfsdk:"access_token"`
	AccessTokenSecret types.String `tfsdk:"access_token_secret"`
}

// New returns a constructor for the provider, as providerserver.Serve expects.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &immobilienscout24Provider{version: version}
	}
}

func (p *immobilienscout24Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "immobilienscout24"
	resp.Version = p.version
}

func (p *immobilienscout24Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	credential := func(description, env string) schema.StringAttribute {
		return schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("%s Can also be set with the `%s` environment variable.", description, env),
			Optional:            true,
			Sensitive:           true,
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages real estate listings through the " +
			"[ImmobilienScout24 Import/Export API](https://api.immobilienscout24.de/api-docs/import-export/introduction/). " +
			"Requests are signed with OAuth 1.0a, using an API key (consumer key and secret) and an access token " +
			"(for your own account, the personal access token and its secret).",
		Attributes: map[string]schema.Attribute{
			"environment": schema.StringAttribute{
				MarkdownDescription: "Which ImmobilienScout24 API to talk to: `sandbox` (the default, " +
					"`" + sandboxBaseURL + "`) or `production` (`" + productionBaseURL + "`). " +
					"API keys are issued per environment.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf(environmentSandbox, environmentProduction),
				},
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "For testing and advanced use only: an absolute `http` or `https` URL that " +
					"replaces the API root (the part before `/offer/v1.0/...`), for example a local fake of the API. " +
					"Conflicts with `environment`.",
				Optional: true,
				Validators: []validator.String{
					absoluteHTTPURL{},
					stringvalidator.ConflictsWith(path.MatchRoot("environment")),
				},
			},
			"consumer_key":        credential("OAuth consumer key (the API key).", "IMMOBILIENSCOUT24_CONSUMER_KEY"),
			"consumer_secret":     credential("OAuth consumer secret of the API key.", "IMMOBILIENSCOUT24_CONSUMER_SECRET"),
			"access_token":        credential("OAuth access token, for example the personal access token.", "IMMOBILIENSCOUT24_ACCESS_TOKEN"),
			"access_token_secret": credential("OAuth access token secret.", "IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET"),
		},
	}
}

func (p *immobilienscout24Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data immobilienscout24ProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, diags := resolveProviderSettings(data, os.Getenv)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := NewClient(settings.baseURL, settings.consumerKey, settings.consumerSecret,
		settings.accessToken, settings.accessTokenSecret,
		"terraform-provider-immobilienscout24/"+p.version)
	resp.DataSourceData = client
	resp.ResourceData = client
}

type providerSettings struct {
	baseURL           string
	consumerKey       string
	consumerSecret    string
	accessToken       string
	accessTokenSecret string
}

// resolveProviderSettings merges the configuration with the environment. A
// value in the configuration wins over the environment variable.
func resolveProviderSettings(data immobilienscout24ProviderModel, getenv func(string) string) (providerSettings, diag.Diagnostics) {
	var diags diag.Diagnostics
	values := []types.String{data.ConsumerKey, data.ConsumerSecret, data.AccessToken, data.AccessTokenSecret}
	all := append([]types.String{data.Environment, data.BaseURL}, values...)
	names := []string{"environment", "base_url"}
	for _, c := range credentialEnv {
		names = append(names, c.attribute)
	}
	for i, v := range all {
		if v.IsUnknown() {
			diags.AddAttributeError(path.Root(names[i]), "Unknown provider configuration value",
				fmt.Sprintf("The provider cannot be configured because %q is not known until apply. "+
					"Set it to a value known at plan time, or use the environment variable instead.", names[i]))
		}
	}
	if diags.HasError() {
		return providerSettings{}, diags
	}

	resolved := make([]string, len(values))
	var missing []string
	for i, v := range values {
		resolved[i] = v.ValueString()
		if resolved[i] == "" {
			resolved[i] = getenv(credentialEnv[i].env)
		}
		if resolved[i] == "" {
			missing = append(missing, fmt.Sprintf("%s (or %s)", credentialEnv[i].attribute, credentialEnv[i].env))
		}
	}
	if len(missing) > 0 {
		diags.AddError("Missing ImmobilienScout24 credentials",
			"Set these provider attributes or environment variables: "+strings.Join(missing, ", ")+".")
		return providerSettings{}, diags
	}

	baseURL := sandboxBaseURL
	switch {
	case data.BaseURL.ValueString() != "":
		baseURL = data.BaseURL.ValueString()
	case data.Environment.ValueString() == environmentProduction:
		baseURL = productionBaseURL
	}

	return providerSettings{
		baseURL:           baseURL,
		consumerKey:       resolved[0],
		consumerSecret:    resolved[1],
		accessToken:       resolved[2],
		accessTokenSecret: resolved[3],
	}, diags
}

func (p *immobilienscout24Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewApartmentRentResource,
		NewAttachmentLinkResource,
		NewAttachmentPDFResource,
		NewAttachmentPictureResource,
		NewContactResource,
		NewPublicationResource,
	}
}

func (p *immobilienscout24Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// absoluteHTTPURL validates that a string is an absolute http or https URL.
type absoluteHTTPURL struct{}

func (absoluteHTTPURL) Description(_ context.Context) string {
	return "value must be an absolute http or https URL"
}

func (v absoluteHTTPURL) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v absoluteHTTPURL) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := checkAbsoluteHTTPURL(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid URL", fmt.Sprintf("%s: %s.", v.Description(ctx), err))
	}
}

func checkAbsoluteHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q does not parse", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q has no http or https scheme", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%q must not contain a query, fragment or user info", raw)
	}
	return nil
}
