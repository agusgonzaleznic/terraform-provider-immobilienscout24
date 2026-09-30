package immobilienscout24

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// salutations is common:SalutationType.
var salutations = []string{"FEMALE", "MALE", "COMPANY", "NO_SALUTATION"}

var (
	// emailPattern is the pattern of common:Email, anchored as XSD patterns are.
	emailPattern = regexp.MustCompile(`^.+@.+\..+$`)
	// phonePattern is the pattern of the combined phone number elements, with a
	// group for the country code, the area code and the subscriber number.
	phonePattern = regexp.MustCompile(`^(\+[1-9]\d{0,3}) +(\d{1,10}) +([\d][\d \-]{0,24}[\d])$`)
	// countryCodePattern matches the shape of common:CountryCode.
	countryCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// checkPhoneNumber applies the rules of the Create a Contact page to a
// combined phone number: "„00“ is not allowed, it needs to be „+"" and "if
// Countrycode = "+49", than we disallow an area code which starts with „0"".
func checkPhoneNumber(s string) error {
	if strings.HasPrefix(s, "00") {
		return fmt.Errorf("%q starts with 00; write the country code with a plus sign, for example +49", s)
	}
	m := phonePattern.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("%q is not a country code, an area code and a subscriber number separated by spaces, "+
			"for example +49 30 24301999", s)
	}
	if m[1] == "+49" && strings.HasPrefix(m[2], "0") {
		return fmt.Errorf("%q: with the country code +49, the area code must not start with 0 (+49 30, not +49 030)", s)
	}
	return nil
}

// checkHTTPURL accepts an absolute http or https URL.
func checkHTTPURL(raw string) error {
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
	return nil
}

// stringCheck validates a string with check and reports its error under summary.
type stringCheck struct {
	description string
	summary     string
	check       func(string) error
}

func (v stringCheck) Description(_ context.Context) string { return v.description }

func (v stringCheck) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v stringCheck) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := v.check(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, v.summary, err.Error()+".")
	}
}

var phoneNumberCheck = stringCheck{
	description: "value must be a phone number such as +49 30 24301999",
	summary:     "Invalid phone number",
	check:       checkPhoneNumber,
}

func phoneNumber(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc + ": the country code, the area code and the subscriber number, separated by spaces, " +
			"for example `+49 30 24301999`. The country code starts with `+`, not `00`, and after `+49` the area code " +
			"has no leading `0`.",
		Optional:   true,
		Validators: []validator.String{phoneNumberCheck},
	}
}

func emailAddress(desc string, required bool) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc + ", 5 to 300 characters.",
		Required:            required,
		Optional:            !required,
		Validators: []validator.String{
			stringvalidator.UTF8LengthBetween(5, 300),
			stringvalidator.RegexMatches(emailPattern, "must be an email address such as anna@example.com"),
		},
	}
}

// onlyTrue rejects false for a flag that ImmobilienScout24 ignores when it is
// false, and reports it with summary and detail.
type onlyTrue struct {
	summary, detail string
}

func (onlyTrue) Description(_ context.Context) string {
	return "value must be true; leave the attribute out otherwise"
}

func (v onlyTrue) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v onlyTrue) ValidateBool(_ context.Context, req validator.BoolRequest, resp *validator.BoolResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.ValueBool() {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, v.summary, v.detail)
}

// defaultContactOnlyTrue rejects default_contact = false.
var defaultContactOnlyTrue = onlyTrue{
	summary: "default_contact cannot be false",
	detail: "An account always has exactly one default contact, and ImmobilienScout24 ignores an update that sets " +
		"defaultContact to false. To move the default, set default_contact = true on another " +
		"immobilienscout24_contact (or choose another default contact on the website), and leave " +
		"default_contact out of this one.",
}

// keepDefaultContactFlag is UseStateForUnknown for default_contact, except on
// the account's default contact: another contact can take the flag from it
// during the same apply that updates it (by being created or updated with
// default_contact = true), so its flag is only known after apply.
type keepDefaultContactFlag struct{}

func (keepDefaultContactFlag) Description(_ context.Context) string {
	return "Keeps a false default_contact from the prior state; a true one can change during apply."
}

func (m keepDefaultContactFlag) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepDefaultContactFlag) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.StateValue.IsNull() || req.StateValue.ValueBool() || !req.PlanValue.IsUnknown() || req.ConfigValue.IsUnknown() {
		return
	}
	resp.PlanValue = req.StateValue
}

// contactSchema is the schema of immobilienscout24_contact. Each description
// names the element the attribute maps to; see contact_xml.go for the wire order.
func contactSchema() schema.Schema {
	return schema.Schema{
		MarkdownDescription: "A contact address (`common:realtorContactDetail`) in the ImmobilienScout24 account of the " +
			"access token. A listing shows one contact: point a listing resource, such as " +
			"`immobilienscout24_apartment_rent`, at this one with `contact_id`.\n\n" +
			"The API treats every update as a full replacement, so the fields this resource does not model are reset " +
			"when Terraform updates the contact: `company`, `officeHours`, `portraitUrl`, `clickOutUrl`, " +
			"`localPartnerContact` and `businessCardContact`. ImmobilienScout24 asks accounts that booked Branchenbuch " +
			"or Image Boost to send `businessCardContact`, so do not manage such a business card contact with this " +
			"resource.\n\n" +
			"Every account has exactly one default contact, which ImmobilienScout24 uses for listings without a " +
			"contact; `default_contact` explains how to move it. Destroying a contact moves the listings that use it " +
			"to the default contact. ImmobilienScout24 refuses to delete the default contact itself.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The contact id that ImmobilienScout24 assigned on creation.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email":      emailAddress("Email address (`email`)", true),
			"salutation": enumWithDefault("Salutation (`salutation`).", salutations, "NO_SALUTATION"),
			"firstname":  optionalStringWithMax("First name (`firstname`), at most 30 characters.", 30),
			"lastname": schema.StringAttribute{
				MarkdownDescription: "Last name (`lastname`), at most 50 characters.",
				Required:            true,
				Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(50)},
			},
			"title":             optionalStringWithMax("Academic title (`title`), such as `Dr.`, at most 15 characters.", 15),
			"addition_name":     optionalStringWithMax("Addition to the name (`additionName`), at most 30 characters.", 30),
			"phone_number":      phoneNumber("Phone number (`phoneNumber`)"),
			"cell_phone_number": phoneNumber("Mobile phone number (`cellPhoneNumber`)"),
			"fax_number":        phoneNumber("Fax number (`faxNumber`)"),
			"address": schema.SingleNestedAttribute{
				MarkdownDescription: "Postal address of the contact (`address`). Set at least one of its attributes.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"street":       optionalStringWithMax("Street (`street`), at most 100 characters.", 100),
					"house_number": optionalStringWithMax("House number (`houseNumber`), at most 30 characters.", 30),
					"postcode":     optionalStringWithMax("Postcode (`postcode`), at most 20 characters.", 20),
					"city":         optionalStringWithMax("City (`city`), at most 50 characters.", 50),
				},
			},
			"country_code": schema.StringAttribute{
				MarkdownDescription: "Country of the contact as an ISO 3166-1 alpha-3 code in upper case (`countryCode`), " +
					"for example `DEU`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(countryCodePattern, "must be three upper-case letters, for example DEU"),
				},
			},
			"homepage_url": schema.StringAttribute{
				MarkdownDescription: "Homepage (`homepageUrl`), an `http` or `https` URL of at most 300 characters.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtMost(300),
					stringCheck{description: "value must be an http or https URL", summary: "Invalid URL", check: checkHTTPURL},
				},
			},
			"position": optionalStringWithMax("Position or function in the company (`position`), at most 100 characters.", 100),
			"secondary_email": emailAddress("A second email address that also receives the enquiries (`secondaryEmail`)",
				false),
			"default_contact": schema.BoolAttribute{
				MarkdownDescription: "Whether this is the account's default contact (`defaultContact`), which " +
					"ImmobilienScout24 uses for every listing without a contact. An account has exactly one. Set it to " +
					"`true` to make this contact the default; the previous default contact loses the flag. It cannot be " +
					"`false`: ImmobilienScout24 ignores that, and the default only moves when another contact becomes the " +
					"default. So to move the default away from this contact, set `default_contact = true` on another " +
					"contact, or choose another default on the website. When left out, the current flag is kept and read " +
					"back; a plan that updates the default contact shows its flag as known after apply, because " +
					"another contact can take the default in the same run. Set it to `true` on one contact only: when " +
					"two contacts both set it, the apply fails with \"Another contact became the default contact\".",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{keepDefaultContactFlag{}},
				Validators:    []validator.Bool{defaultContactOnlyTrue},
			},
			"external_id": schema.StringAttribute{
				MarkdownDescription: "Your own id for the contact (`externalId`), unique within the account.",
				Optional:            true,
				Validators:          []validator.String{nonEmpty},
			},
			"show_on_profile_page": boolDefaultFalse("Whether the contact is shown on the realtor's profile page " +
				"(`showOnProfilePage`). ImmobilienScout24 allows this for at most 20 contacts per account."),
		},
	}
}
