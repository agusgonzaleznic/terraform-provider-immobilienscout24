package immobilienscout24

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"immobilienscout24": providerserver.NewProtocol6WithError(New("test")()),
}

func noEnv(string) string { return "" }

func TestResolveProviderSettingsDefaultsAndOverrides(t *testing.T) {
	creds := immobilienscout24ProviderModel{
		ConsumerKey: types.StringValue("ck"), ConsumerSecret: types.StringValue("cs"),
		AccessToken: types.StringValue("at"), AccessTokenSecret: types.StringValue("ats"),
	}
	for name, tc := range map[string]struct {
		environment, baseURL, want string
	}{
		"default is sandbox": {want: sandboxBaseURL},
		"sandbox":            {environment: "sandbox", want: sandboxBaseURL},
		"production":         {environment: "production", want: productionBaseURL},
		"base_url wins":      {baseURL: "http://127.0.0.1:1/restapi/api", want: "http://127.0.0.1:1/restapi/api"},
	} {
		t.Run(name, func(t *testing.T) {
			m := creds
			m.Environment, m.BaseURL = types.StringNull(), types.StringNull()
			if tc.environment != "" {
				m.Environment = types.StringValue(tc.environment)
			}
			if tc.baseURL != "" {
				m.BaseURL = types.StringValue(tc.baseURL)
			}
			got, diags := resolveProviderSettings(m, noEnv)
			if diags.HasError() {
				t.Fatal(diags)
			}
			if got.baseURL != tc.want {
				t.Fatalf("baseURL = %q, want %q", got.baseURL, tc.want)
			}
		})
	}
}

func TestResolveProviderSettingsEnvironmentFallback(t *testing.T) {
	env := map[string]string{
		"IMMOBILIENSCOUT24_CONSUMER_KEY":        "env-ck",
		"IMMOBILIENSCOUT24_CONSUMER_SECRET":     "env-cs",
		"IMMOBILIENSCOUT24_ACCESS_TOKEN":        "env-at",
		"IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET": "env-ats",
	}
	m := immobilienscout24ProviderModel{ConsumerKey: types.StringValue("config-ck")}
	got, diags := resolveProviderSettings(m, func(k string) string { return env[k] })
	if diags.HasError() {
		t.Fatal(diags)
	}
	if got.consumerKey != "config-ck" || got.consumerSecret != "env-cs" || got.accessToken != "env-at" || got.accessTokenSecret != "env-ats" {
		t.Fatalf("unexpected settings: %+v", got)
	}
}

func TestResolveProviderSettingsNamesMissingCredentials(t *testing.T) {
	m := immobilienscout24ProviderModel{ConsumerKey: types.StringValue("ck"), AccessToken: types.StringValue("at")}
	_, diags := resolveProviderSettings(m, noEnv)
	if !diags.HasError() {
		t.Fatal("expected an error")
	}
	detail := diags.Errors()[0].Detail()
	for _, want := range []string{"consumer_secret (or IMMOBILIENSCOUT24_CONSUMER_SECRET)", "access_token_secret (or IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET)"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q lacks %q", detail, want)
		}
	}
	if strings.Contains(detail, "consumer_key") || strings.Contains(detail, "access_token (") {
		t.Errorf("detail names a credential that is set: %q", detail)
	}
}

func TestResolveProviderSettingsUnknownValues(t *testing.T) {
	m := immobilienscout24ProviderModel{ConsumerKey: types.StringUnknown()}
	_, diags := resolveProviderSettings(m, noEnv)
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Summary(), "Unknown") {
		t.Fatalf("expected an unknown-value error, got %v", diags)
	}
}

func TestCheckAbsoluteHTTPURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://rest.sandbox-immobilienscout24.de/restapi/api": true,
		"http://127.0.0.1:8080/restapi/api":                     true,
		"rest.immobilienscout24.de/restapi/api":                 false,
		"/restapi/api":                                          false,
		"ftp://example.com":                                     false,
		"https://":                                              false,
		"https://example.com/?a=b":                              false,
		"https://user:pw@example.com":                           false,
	} {
		if err := checkAbsoluteHTTPURL(raw); (err == nil) != ok {
			t.Errorf("checkAbsoluteHTTPURL(%q) = %v, want ok=%v", raw, err, ok)
		}
	}
}
