package immobilienscout24

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type apartmentRentModel struct {
	ID                          types.String   `tfsdk:"id"`
	ExternalID                  types.String   `tfsdk:"external_id"`
	Title                       types.String   `tfsdk:"title"`
	Address                     *addressModel  `tfsdk:"address"`
	ShowAddress                 types.Bool     `tfsdk:"show_address"`
	DescriptionNote             types.String   `tfsdk:"description_note"`
	FurnishingNote              types.String   `tfsdk:"furnishing_note"`
	LocationNote                types.String   `tfsdk:"location_note"`
	OtherNote                   types.String   `tfsdk:"other_note"`
	ApartmentType               types.String   `tfsdk:"apartment_type"`
	Floor                       types.Int64    `tfsdk:"floor"`
	Lift                        types.Bool     `tfsdk:"lift"`
	Cellar                      types.String   `tfsdk:"cellar"`
	FreeFrom                    types.String   `tfsdk:"free_from"`
	NumberOfFloors              types.Int64    `tfsdk:"number_of_floors"`
	BaseRent                    types.Float64  `tfsdk:"base_rent"`
	TotalRent                   types.Float64  `tfsdk:"total_rent"`
	ServiceCharge               types.Float64  `tfsdk:"service_charge"`
	Deposit                     types.String   `tfsdk:"deposit"`
	HeatingCosts                types.Float64  `tfsdk:"heating_costs"`
	HeatingCostsInServiceCharge types.String   `tfsdk:"heating_costs_in_service_charge"`
	PetsAllowed                 types.String   `tfsdk:"pets_allowed"`
	LivingSpace                 types.Float64  `tfsdk:"living_space"`
	NumberOfRooms               types.Float64  `tfsdk:"number_of_rooms"`
	BuiltInKitchen              types.Bool     `tfsdk:"built_in_kitchen"`
	Balcony                     types.Bool     `tfsdk:"balcony"`
	Garden                      types.Bool     `tfsdk:"garden"`
	Courtage                    *courtageModel `tfsdk:"courtage"`
}

type addressModel struct {
	Street      types.String      `tfsdk:"street"`
	HouseNumber types.String      `tfsdk:"house_number"`
	Postcode    types.String      `tfsdk:"postcode"`
	City        types.String      `tfsdk:"city"`
	Coordinates *coordinatesModel `tfsdk:"coordinates"`
}

type coordinatesModel struct {
	Latitude  types.Float64 `tfsdk:"latitude"`
	Longitude types.Float64 `tfsdk:"longitude"`
}

type courtageModel struct {
	HasCourtage  types.String `tfsdk:"has_courtage"`
	Courtage     types.String `tfsdk:"courtage"`
	CourtageNote types.String `tfsdk:"courtage_note"`
}

// toDocument builds the complete request document from a plan. Null and
// unknown optional values are left out, which on PUT clears them.
func (m *apartmentRentModel) toDocument() *apartmentRentDocument {
	f := apartmentRentFields{
		ExternalID:                  m.ExternalID.ValueString(),
		Title:                       m.Title.ValueString(),
		DescriptionNote:             m.DescriptionNote.ValueString(),
		FurnishingNote:              m.FurnishingNote.ValueString(),
		LocationNote:                m.LocationNote.ValueString(),
		OtherNote:                   m.OtherNote.ValueString(),
		ShowAddress:                 m.ShowAddress.ValueBoolPointer(),
		ApartmentType:               m.ApartmentType.ValueString(),
		Floor:                       formatInt(m.Floor),
		Lift:                        m.Lift.ValueBoolPointer(),
		Cellar:                      m.Cellar.ValueString(),
		FreeFrom:                    m.FreeFrom.ValueString(),
		NumberOfFloors:              formatInt(m.NumberOfFloors),
		BaseRent:                    formatFloat(m.BaseRent),
		TotalRent:                   formatFloat(m.TotalRent),
		ServiceCharge:               formatFloat(m.ServiceCharge),
		Deposit:                     m.Deposit.ValueString(),
		HeatingCosts:                formatFloat(m.HeatingCosts),
		HeatingCostsInServiceCharge: m.HeatingCostsInServiceCharge.ValueString(),
		PetsAllowed:                 m.PetsAllowed.ValueString(),
		LivingSpace:                 formatFloat(m.LivingSpace),
		NumberOfRooms:               formatFloat(m.NumberOfRooms),
		BuiltInKitchen:              m.BuiltInKitchen.ValueBoolPointer(),
		Balcony:                     m.Balcony.ValueBoolPointer(),
		Garden:                      m.Garden.ValueBoolPointer(),
	}
	if a := m.Address; a != nil {
		f.Address = &addressElement{
			Street:      a.Street.ValueString(),
			HouseNumber: a.HouseNumber.ValueString(),
			Postcode:    a.Postcode.ValueString(),
			City:        a.City.ValueString(),
		}
		if c := a.Coordinates; c != nil {
			f.Address.Wgs84Coordinate = &coordinateElement{
				Latitude:  *formatFloat(c.Latitude),
				Longitude: *formatFloat(c.Longitude),
			}
		}
	}
	if c := m.Courtage; c != nil {
		f.Courtage = &courtageElement{
			HasCourtage:  c.HasCourtage.ValueString(),
			Courtage:     c.Courtage.ValueString(),
			CourtageNote: c.CourtageNote.ValueString(),
		}
	}
	return &apartmentRentDocument{apartmentRentFields: f}
}

// fromDocument maps a GET response onto the model. prior is the model the
// values are compared against (the plan after a write, the state on refresh,
// an id-only state on import). Numbers that are equal to the prior value keep
// the prior value, so that formatting such as 100000.00 versus 100000 never
// shows up as a difference.
func fromDocument(id string, doc *apartmentRentDocument, prior *apartmentRentModel) (*apartmentRentModel, error) {
	p := &parser{}
	m := &apartmentRentModel{
		ID:                          types.StringValue(id),
		ExternalID:                  optionalString(doc.ExternalID),
		Title:                       keepText(doc.Title, prior.Title, true),
		ShowAddress:                 types.BoolPointerValue(doc.ShowAddress),
		DescriptionNote:             keepText(doc.DescriptionNote, prior.DescriptionNote, false),
		FurnishingNote:              keepText(doc.FurnishingNote, prior.FurnishingNote, false),
		LocationNote:                keepText(doc.LocationNote, prior.LocationNote, false),
		OtherNote:                   keepText(doc.OtherNote, prior.OtherNote, false),
		ApartmentType:               optionalString(doc.ApartmentType),
		Floor:                       p.int64("floor", doc.Floor),
		Lift:                        types.BoolPointerValue(doc.Lift),
		Cellar:                      optionalString(doc.Cellar),
		FreeFrom:                    optionalString(doc.FreeFrom),
		NumberOfFloors:              p.int64("numberOfFloors", doc.NumberOfFloors),
		BaseRent:                    p.float64("baseRent", doc.BaseRent, prior.BaseRent),
		TotalRent:                   p.float64("totalRent", doc.TotalRent, prior.TotalRent),
		ServiceCharge:               p.float64("serviceCharge", doc.ServiceCharge, prior.ServiceCharge),
		Deposit:                     optionalString(doc.Deposit),
		HeatingCosts:                p.float64("heatingCosts", doc.HeatingCosts, prior.HeatingCosts),
		HeatingCostsInServiceCharge: optionalString(doc.HeatingCostsInServiceCharge),
		PetsAllowed:                 optionalString(doc.PetsAllowed),
		LivingSpace:                 p.float64("livingSpace", doc.LivingSpace, prior.LivingSpace),
		NumberOfRooms:               p.float64("numberOfRooms", doc.NumberOfRooms, prior.NumberOfRooms),
		BuiltInKitchen:              types.BoolPointerValue(doc.BuiltInKitchen),
		Balcony:                     types.BoolPointerValue(doc.Balcony),
		Garden:                      types.BoolPointerValue(doc.Garden),
	}

	if a := doc.Address; a != nil {
		pa := prior.Address
		if pa == nil {
			pa = &addressModel{}
		}
		m.Address = &addressModel{
			Street:      keepText(a.Street, pa.Street, true),
			HouseNumber: keepText(a.HouseNumber, pa.HouseNumber, true),
			Postcode:    keepText(a.Postcode, pa.Postcode, true),
			City:        keepText(a.City, pa.City, true),
		}
		// ImmobilienScout24 geocodes addresses that come without coordinates
		// (seen on the sandbox, 2026-09-29), so coordinates in a response are
		// only managed when the prior model had them. That includes import:
		// otherwise every import of a geocoded object would differ from a
		// configuration without coordinates.
		priorCoords := (*coordinatesModel)(nil)
		manage := false
		if prior.Address != nil && prior.Address.Coordinates != nil {
			priorCoords, manage = prior.Address.Coordinates, true
		}
		if c := a.Wgs84Coordinate; c != nil && manage {
			if priorCoords == nil {
				priorCoords = &coordinatesModel{}
			}
			lat, lon := strings.TrimSpace(c.Latitude), strings.TrimSpace(c.Longitude)
			m.Address.Coordinates = &coordinatesModel{
				Latitude:  p.float64("wgs84Coordinate.latitude", &lat, priorCoords.Latitude),
				Longitude: p.float64("wgs84Coordinate.longitude", &lon, priorCoords.Longitude),
			}
		}
	}
	if c := doc.Courtage; c != nil {
		m.Courtage = &courtageModel{
			HasCourtage:  types.StringValue(c.HasCourtage),
			Courtage:     optionalString(c.Courtage),
			CourtageNote: optionalString(c.CourtageNote),
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}

// keepText maps a returned text onto the model. When it only differs from the
// prior value in letter case or surrounding whitespace, the prior value is kept:
// the documented GET example returns "andreasstr" and "berlin" for an object
// inserted as "Andreasstr" and "Berlin", and a heredoc ends in a newline the API
// may not keep. Without this, every apply would end in an inconsistent result.
func keepText(returned string, prior types.String, required bool) types.String {
	if !prior.IsNull() && !prior.IsUnknown() &&
		strings.EqualFold(strings.TrimSpace(returned), strings.TrimSpace(prior.ValueString())) {
		return prior
	}
	if required {
		return types.StringValue(returned)
	}
	return optionalString(returned)
}

func optionalString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func formatFloat(v types.Float64) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := strconv.FormatFloat(v.ValueFloat64(), 'f', -1, 64)
	return &s
}

func formatInt(v types.Int64) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := strconv.FormatInt(v.ValueInt64(), 10)
	return &s
}

// parser collects the first number that does not parse.
type parser struct {
	err error
}

func (p *parser) number(element string, raw *string) (float64, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(*raw), 64)
	if err != nil && p.err == nil {
		p.err = fmt.Errorf("the API returned %q for %s, which is not a number", *raw, element)
	}
	return v, err == nil
}

func (p *parser) float64(element string, raw *string, prior types.Float64) types.Float64 {
	v, ok := p.number(element, raw)
	if !ok {
		return types.Float64Null()
	}
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueFloat64() == v {
		return prior
	}
	return types.Float64Value(v)
}

// int64 accepts integral decimals such as "4.0" as well as "4".
func (p *parser) int64(element string, raw *string) types.Int64 {
	v, ok := p.number(element, raw)
	if !ok {
		return types.Int64Null()
	}
	if v != math.Trunc(v) {
		if p.err == nil {
			p.err = fmt.Errorf("the API returned %q for %s, which is not a whole number", *raw, element)
		}
		return types.Int64Null()
	}
	return types.Int64Value(int64(v))
}
