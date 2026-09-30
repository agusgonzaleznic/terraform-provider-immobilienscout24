package immobilienscout24

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// listingModel holds the attributes that every listing resource has. The
// model of each listing type embeds it.
type listingModel struct {
	ID              types.String   `tfsdk:"id"`
	ExternalID      types.String   `tfsdk:"external_id"`
	Title           types.String   `tfsdk:"title"`
	Address         *addressModel  `tfsdk:"address"`
	ShowAddress     types.Bool     `tfsdk:"show_address"`
	ContactID       types.String   `tfsdk:"contact_id"`
	DescriptionNote types.String   `tfsdk:"description_note"`
	FurnishingNote  types.String   `tfsdk:"furnishing_note"`
	LocationNote    types.String   `tfsdk:"location_note"`
	OtherNote       types.String   `tfsdk:"other_note"`
	Courtage        *courtageModel `tfsdk:"courtage"`

	// The elements of buildingFields, see listing_energy.go.
	EnergyCertificate                  *energyCertificateModel `tfsdk:"energy_certificate"`
	Cellar                             types.String            `tfsdk:"cellar"`
	ConstructionYear                   types.Int64             `tfsdk:"construction_year"`
	FreeFrom                           types.String            `tfsdk:"free_from"`
	HeatingType                        types.String            `tfsdk:"heating_type"`
	EnergySources                      types.Set               `tfsdk:"energy_sources"`
	BuildingEnergyRatingType           types.String            `tfsdk:"building_energy_rating_type"`
	ThermalCharacteristic              types.Float64           `tfsdk:"thermal_characteristic"`
	EnergyConsumptionContainsWarmWater types.String            `tfsdk:"energy_consumption_contains_warm_water"`
	NumberOfFloors                     types.Int64             `tfsdk:"number_of_floors"`
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

// listing returns the attributes every listing type has. It is promoted to
// the model of each type, which is how the shared CRUD flow in
// resource_listing.go reaches them.
func (m *listingModel) listing() *listingModel { return m }

// toListingFields builds the elements every listing type starts with. Null
// and unknown optional values are left out, which on PUT clears them. That
// includes the contact: a PUT without one resets the listing to the default
// contact (observed 2026-09-29), so Update fills in the listing's current
// contact first when the configuration leaves contact_id out.
func (m *listingModel) toListingFields() listingFields {
	f := listingFields{
		ExternalID:      m.ExternalID.ValueString(),
		Title:           m.Title.ValueString(),
		DescriptionNote: m.DescriptionNote.ValueString(),
		FurnishingNote:  m.FurnishingNote.ValueString(),
		LocationNote:    m.LocationNote.ValueString(),
		OtherNote:       m.OtherNote.ValueString(),
		ShowAddress:     m.ShowAddress.ValueBoolPointer(),
	}
	if !m.ContactID.IsNull() && !m.ContactID.IsUnknown() {
		f.Contact = &idElement{ID: m.ContactID.ValueString()}
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
	return f
}

// toCourtage builds the courtage element, which each listing type has at a
// position of its own.
func (m *listingModel) toCourtage() *courtageElement {
	c := m.Courtage
	if c == nil {
		return nil
	}
	return &courtageElement{
		HasCourtage:  c.HasCourtage.ValueString(),
		Courtage:     c.Courtage.ValueString(),
		CourtageNote: c.CourtageNote.ValueString(),
	}
}

// toPrice builds the price of a sale, which is in EUR.
func toPrice(v types.Float64) *priceElement {
	value := formatFloat(v)
	if value == nil {
		return nil
	}
	return &priceElement{Value: value, Currency: "EUR"}
}

// readListing maps the elements that every listing type has onto the shared
// attributes. prior is the model the values are compared against (the plan
// after a write, the state on refresh, an id-only state on import). Numbers
// that are equal to the prior value keep the prior value, so that formatting
// such as 100000.00 versus 100000 never shows up as a difference.
func readListing(p *parser, id string, f *listingFields, courtage *courtageElement, b *buildingFields, prior *listingModel) listingModel {
	m := listingModel{
		ID:              types.StringValue(id),
		ExternalID:      optionalString(f.ExternalID),
		Title:           keepText(f.Title, prior.Title, true),
		ShowAddress:     types.BoolPointerValue(f.ShowAddress),
		DescriptionNote: keepText(f.DescriptionNote, prior.DescriptionNote, false),
		FurnishingNote:  keepText(f.FurnishingNote, prior.FurnishingNote, false),
		LocationNote:    keepText(f.LocationNote, prior.LocationNote, false),
		OtherNote:       keepText(f.OtherNote, prior.OtherNote, false),
	}

	if a := f.Address; a != nil {
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
	// Every GET names the listing's contact, the default contact included
	// (observed 2026-09-29), as <contact id="..." externalId="..."/>.
	if c := f.Contact; c != nil {
		m.ContactID = optionalString(strings.TrimSpace(c.ID))
	}
	if c := courtage; c != nil {
		m.Courtage = &courtageModel{
			HasCourtage:  types.StringValue(c.HasCourtage),
			Courtage:     optionalString(c.Courtage),
			CourtageNote: optionalString(c.CourtageNote),
		}
	}
	readBuilding(p, b, prior, &m)
	return m
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
