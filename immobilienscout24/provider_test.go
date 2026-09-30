package immobilienscout24

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
		// http only for a loopback host.
		"http://localhost:8080/restapi/api":            true,
		"http://LocalHost/restapi/api":                 true,
		"http://127.1.2.3:8080/restapi/api":            true,
		"http://[::1]:8080/restapi/api":                true,
		"https://localhost/restapi/api":                true,
		"http://rest.immobilienscout24.de/restapi/api": false,
		"http://10.0.0.1:8080/restapi/api":             false,
		"http://localhost.example.com/restapi/api":     false,
		"http://[::2]:8080/restapi/api":                false,
	} {
		if err := checkAbsoluteHTTPURL(raw); (err == nil) != ok {
			t.Errorf("checkAbsoluteHTTPURL(%q) = %v, want ok=%v", raw, err, ok)
		}
	}
}

// No error about base_url repeats a secret that the value holds: a password,
// a user name used as a token, a query or fragment, or anything of a value
// that does not parse.
func TestBaseURLErrorsDoNotRepeatSecrets(t *testing.T) {
	for _, raw := range []string{
		"https://user:s3cr3t@example.com/restapi/api", // trufflehog:ignore (a fake credential under test)
		"http://user:s3cr3t@example.com/restapi/api",  // trufflehog:ignore (a fake credential under test)
		"https://s3cr3t@example.com/restapi/api",
		"https://example.com/restapi/api?token=s3cr3t",
		"https://example.com/restapi/api#s3cr3t",
		"http://example.com/restapi/api?token=s3cr3t",
		"ftp://user:s3cr3t@example.com", // trufflehog:ignore (a fake credential under test)
		"https:user:s3cr3t@example.com",
		"https://user:s3cr3t@exa mple.com/restapi/api",
	} {
		err := checkAbsoluteHTTPURL(raw)
		if err == nil {
			t.Errorf("%q: accepted", raw)
			continue
		}
		resp := &validator.StringResponse{}
		absoluteHTTPURL{}.ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("base_url"), ConfigValue: types.StringValue(raw),
		}, resp)
		detail := resp.Diagnostics.Errors()[0].Detail()
		for _, text := range []string{err.Error(), detail} {
			if strings.Contains(text, "s3cr3t") || strings.Contains(text, "exa mple") {
				t.Errorf("%q: the error repeats the value: %s", raw, text)
			}
		}
	}
	if err := checkAbsoluteHTTPURL("https://user:s3cr3t@exa mple.com"); err == nil || err.Error() != "the value does not parse as a URL" {
		t.Errorf("a value that does not parse: %v", err)
	}
}
