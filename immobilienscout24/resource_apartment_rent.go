package immobilienscout24

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// apartmentRentKind is immobilienscout24_apartment_rent, a
// realestates:apartmentRent.
var apartmentRentKind = &listingKind[apartmentRentModel, apartmentRentDocument]{
	typeName:       "_apartment_rent",
	realEstateType: realEstateType{root: "apartmentRent", plural: "apartment rentals"},
	noun:           "apartment for rent",
	schema:         apartmentRentSchema,
	listing:        (*apartmentRentModel).listing,
	toDocument:     (*apartmentRentModel).toDocument,
	toModel:        (*apartmentRentDocument).toModel,
	validate:       validateHeatingCosts,
}

// NewApartmentRentResource returns the immobilienscout24_apartment_rent resource.
func NewApartmentRentResource() resource.Resource {
	return &listingResource[apartmentRentModel, apartmentRentDocument]{kind: apartmentRentKind}
}

func apartmentRentSchema() schema.Schema {
	return listingSchema("An apartment for rent (`realestates:apartmentRent`)", "apartment",
		apartmentAttributes(), rentAttributes(), roomAttributes())
}

type apartmentRentModel struct {
	listingModel
	ApartmentType               types.String  `tfsdk:"apartment_type"`
	Floor                       types.Int64   `tfsdk:"floor"`
	Lift                        types.Bool    `tfsdk:"lift"`
	BaseRent                    types.Float64 `tfsdk:"base_rent"`
	TotalRent                   types.Float64 `tfsdk:"total_rent"`
	ServiceCharge               types.Float64 `tfsdk:"service_charge"`
	Deposit                     types.String  `tfsdk:"deposit"`
	HeatingCosts                types.Float64 `tfsdk:"heating_costs"`
	HeatingCostsInServiceCharge types.String  `tfsdk:"heating_costs_in_service_charge"`
	PetsAllowed                 types.String  `tfsdk:"pets_allowed"`
	LivingSpace                 types.Float64 `tfsdk:"living_space"`
	NumberOfRooms               types.Float64 `tfsdk:"number_of_rooms"`
	BuiltInKitchen              types.Bool    `tfsdk:"built_in_kitchen"`
	Balcony                     types.Bool    `tfsdk:"balcony"`
	Garden                      types.Bool    `tfsdk:"garden"`
}

// apartmentRentDocument is a realestates:apartmentRent; see listingFields for
// the field order.
type apartmentRentDocument struct {
	listingFields                                // 1 to 19
	ApartmentType               string           `xml:"apartmentType,omitempty"` // 20 ApartmentRent
	Floor                       *string          `xml:"floor"`                   // 21
	Lift                        *bool            `xml:"lift"`                    // 22
	buildingFields                               // 24 to 41
	BaseRent                    *string          `xml:"baseRent"`                              // 47
	TotalRent                   *string          `xml:"totalRent"`                             // 48
	ServiceCharge               *string          `xml:"serviceCharge"`                         // 49
	Deposit                     string           `xml:"deposit,omitempty"`                     // 50
	HeatingCosts                *string          `xml:"heatingCosts"`                          // 51
	HeatingCostsInServiceCharge string           `xml:"heatingCostsInServiceCharge,omitempty"` // 52
	PetsAllowed                 string           `xml:"petsAllowed,omitempty"`                 // 53
	LivingSpace                 *string          `xml:"livingSpace"`                           // 57
	NumberOfRooms               *string          `xml:"numberOfRooms"`                         // 58
	BuiltInKitchen              *bool            `xml:"builtInKitchen"`                        // 60
	Balcony                     *bool            `xml:"balcony"`                               // 61
	Garden                      *bool            `xml:"garden"`                                // 63
	Courtage                    *courtageElement `xml:"courtage"`                              // 64
}

// toDocument builds the complete request document from a plan; see
// toListingFields.
func (m *apartmentRentModel) toDocument() *apartmentRentDocument {
	return &apartmentRentDocument{
		listingFields:               m.toListingFields(),
		ApartmentType:               m.ApartmentType.ValueString(),
		Floor:                       formatInt(m.Floor),
		Lift:                        m.Lift.ValueBoolPointer(),
		buildingFields:              m.toBuildingFields(),
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
		Courtage:                    m.toCourtage(),
	}
}

// toModel maps a GET response onto the model; see readListing for prior.
func (d *apartmentRentDocument) toModel(id string, prior *apartmentRentModel) (*apartmentRentModel, error) {
	p := &parser{}
	m := &apartmentRentModel{
		listingModel:                readListing(p, id, &d.listingFields, d.Courtage, &d.buildingFields, &prior.listingModel),
		ApartmentType:               optionalString(d.ApartmentType),
		Floor:                       p.int64("floor", d.Floor),
		Lift:                        types.BoolPointerValue(d.Lift),
		BaseRent:                    p.float64("baseRent", d.BaseRent, prior.BaseRent),
		TotalRent:                   p.float64("totalRent", d.TotalRent, prior.TotalRent),
		ServiceCharge:               p.float64("serviceCharge", d.ServiceCharge, prior.ServiceCharge),
		Deposit:                     optionalString(d.Deposit),
		HeatingCosts:                p.float64("heatingCosts", d.HeatingCosts, prior.HeatingCosts),
		HeatingCostsInServiceCharge: optionalString(d.HeatingCostsInServiceCharge),
		PetsAllowed:                 optionalString(d.PetsAllowed),
		LivingSpace:                 p.float64("livingSpace", d.LivingSpace, prior.LivingSpace),
		NumberOfRooms:               p.float64("numberOfRooms", d.NumberOfRooms, prior.NumberOfRooms),
		BuiltInKitchen:              types.BoolPointerValue(d.BuiltInKitchen),
		Balcony:                     types.BoolPointerValue(d.Balcony),
		Garden:                      types.BoolPointerValue(d.Garden),
	}
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}
