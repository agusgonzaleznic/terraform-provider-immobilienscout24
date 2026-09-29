package immobilienscout24

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type contactModel struct {
	ID                types.String         `tfsdk:"id"`
	Email             types.String         `tfsdk:"email"`
	Salutation        types.String         `tfsdk:"salutation"`
	Firstname         types.String         `tfsdk:"firstname"`
	Lastname          types.String         `tfsdk:"lastname"`
	Title             types.String         `tfsdk:"title"`
	AdditionName      types.String         `tfsdk:"addition_name"`
	PhoneNumber       types.String         `tfsdk:"phone_number"`
	CellPhoneNumber   types.String         `tfsdk:"cell_phone_number"`
	FaxNumber         types.String         `tfsdk:"fax_number"`
	Address           *contactAddressModel `tfsdk:"address"`
	CountryCode       types.String         `tfsdk:"country_code"`
	HomepageURL       types.String         `tfsdk:"homepage_url"`
	Position          types.String         `tfsdk:"position"`
	SecondaryEmail    types.String         `tfsdk:"secondary_email"`
	DefaultContact    types.Bool           `tfsdk:"default_contact"`
	ExternalID        types.String         `tfsdk:"external_id"`
	ShowOnProfilePage types.Bool           `tfsdk:"show_on_profile_page"`
}

type contactAddressModel struct {
	Street      types.String `tfsdk:"street"`
	HouseNumber types.String `tfsdk:"house_number"`
	Postcode    types.String `tfsdk:"postcode"`
	City        types.String `tfsdk:"city"`
}

// toDocument builds the complete request document from a plan. Null optional
// values are left out, which on PUT clears them. defaultContact is sent only
// when makeDefault is set, that is when the configuration says true: the API
// ignores false, and leaving the element out keeps the current flag.
func (m *contactModel) toDocument(makeDefault bool) *contactDocument {
	f := contactFields{
		Email:             m.Email.ValueString(),
		Salutation:        m.Salutation.ValueString(),
		Firstname:         m.Firstname.ValueString(),
		Lastname:          m.Lastname.ValueString(),
		FaxNumber:         m.FaxNumber.ValueString(),
		PhoneNumber:       m.PhoneNumber.ValueString(),
		CellPhoneNumber:   m.CellPhoneNumber.ValueString(),
		CountryCode:       m.CountryCode.ValueString(),
		Title:             m.Title.ValueString(),
		AdditionName:      m.AdditionName.ValueString(),
		HomepageURL:       m.HomepageURL.ValueString(),
		Position:          m.Position.ValueString(),
		SecondaryEmail:    m.SecondaryEmail.ValueString(),
		ExternalID:        m.ExternalID.ValueString(),
		ShowOnProfilePage: m.ShowOnProfilePage.ValueBoolPointer(),
	}
	if makeDefault {
		t := true
		f.DefaultContact = &t
	}
	if a := m.Address; a != nil {
		f.Address = &addressElement{
			Street:      a.Street.ValueString(),
			HouseNumber: a.HouseNumber.ValueString(),
			Postcode:    a.Postcode.ValueString(),
			City:        a.City.ValueString(),
		}
	}
	return &contactDocument{contactFields: f}
}

// contactFromDocument maps a GET response onto the model. The sandbox returns
// every field as it was sent (observed 2026-09-29), so, unlike an apartment,
// nothing is compared with a prior value.
func contactFromDocument(id string, doc *contactDocument) *contactModel {
	m := &contactModel{
		ID:                types.StringValue(id),
		Email:             types.StringValue(doc.Email),
		Salutation:        optionalString(doc.Salutation),
		Firstname:         optionalString(doc.Firstname),
		Lastname:          types.StringValue(doc.Lastname),
		Title:             optionalString(doc.Title),
		AdditionName:      optionalString(doc.AdditionName),
		PhoneNumber:       optionalString(doc.PhoneNumber),
		CellPhoneNumber:   optionalString(doc.CellPhoneNumber),
		FaxNumber:         optionalString(doc.FaxNumber),
		CountryCode:       optionalString(doc.CountryCode),
		HomepageURL:       optionalString(doc.HomepageURL),
		Position:          optionalString(doc.Position),
		SecondaryEmail:    optionalString(doc.SecondaryEmail),
		DefaultContact:    types.BoolPointerValue(doc.DefaultContact),
		ExternalID:        optionalString(doc.ExternalID),
		ShowOnProfilePage: types.BoolPointerValue(doc.ShowOnProfilePage),
	}
	if a := doc.Address; a != nil && (a.Street != "" || a.HouseNumber != "" || a.Postcode != "" || a.City != "") {
		m.Address = &contactAddressModel{
			Street:      optionalString(a.Street),
			HouseNumber: optionalString(a.HouseNumber),
			Postcode:    optionalString(a.Postcode),
			City:        optionalString(a.City),
		}
	}
	return m
}
