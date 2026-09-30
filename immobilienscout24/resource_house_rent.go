package immobilienscout24

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// houseRentKind is immobilienscout24_house_rent, a realestates:houseRent.
var houseRentKind = &listingKind[houseRentModel, houseRentDocument]{
	typeName:       "_house_rent",
	realEstateType: realEstateType{root: "houseRent", plural: "houses for rent"},
	noun:           "house for rent",
	schema:         houseRentSchema,
	listing:        (*houseRentModel).listing,
	toDocument:     (*houseRentModel).toDocument,
	toModel:        (*houseRentDocument).toModel,
	validate:       validateHeatingCosts,
}

// NewHouseRentResource returns the immobilienscout24_house_rent resource.
func NewHouseRentResource() resource.Resource {
	return &listingResource[houseRentModel, houseRentDocument]{kind: houseRentKind}
}

func houseRentSchema() schema.Schema {
	return listingSchema("A house for rent (`realestates:houseRent`)", "house",
		houseAttributes(), rentAttributes(), roomAttributes(), map[string]schema.Attribute{
			"built_in_kitchen": builtInKitchenAttribute(),
		})
}

type houseRentModel struct {
	listingModel
	LivingSpace                 types.Float64 `tfsdk:"living_space"`
	PlotArea                    types.Float64 `tfsdk:"plot_area"`
	NumberOfRooms               types.Float64 `tfsdk:"number_of_rooms"`
	BuildingType                types.String  `tfsdk:"building_type"`
	BaseRent                    types.Float64 `tfsdk:"base_rent"`
	TotalRent                   types.Float64 `tfsdk:"total_rent"`
	ServiceCharge               types.Float64 `tfsdk:"service_charge"`
	Deposit                     types.String  `tfsdk:"deposit"`
	HeatingCosts                types.Float64 `tfsdk:"heating_costs"`
	HeatingCostsInServiceCharge types.String  `tfsdk:"heating_costs_in_service_charge"`
	PetsAllowed                 types.String  `tfsdk:"pets_allowed"`
	BuiltInKitchen              types.Bool    `tfsdk:"built_in_kitchen"`
}

// houseRentDocument is a realestates:houseRent; see listingFields for the
// field order. buildingType is required, and numberOfRooms is an xs:string in
// this type.
type houseRentDocument struct {
	listingFields                                // 1 to 19
	LivingSpace                 *string          `xml:"livingSpace"`            // 21 HouseRent
	PlotArea                    *string          `xml:"plotArea"`               // 22
	NumberOfRooms               *string          `xml:"numberOfRooms"`          // 23
	Courtage                    *courtageElement `xml:"courtage"`               // 25
	BuildingType                string           `xml:"buildingType,omitempty"` // 26
	buildingFields                               // 27 to 44
	BaseRent                    *string          `xml:"baseRent"`                              // 50
	TotalRent                   *string          `xml:"totalRent"`                             // 51
	ServiceCharge               *string          `xml:"serviceCharge"`                         // 52
	Deposit                     string           `xml:"deposit,omitempty"`                     // 53
	HeatingCosts                *string          `xml:"heatingCosts"`                          // 54
	HeatingCostsInServiceCharge string           `xml:"heatingCostsInServiceCharge,omitempty"` // 55
	PetsAllowed                 string           `xml:"petsAllowed,omitempty"`                 // 56
	BuiltInKitchen              *bool            `xml:"builtInKitchen"`                        // 59
}

// toDocument builds the complete request document from a plan; see
// toListingFields.
func (m *houseRentModel) toDocument() *houseRentDocument {
	return &houseRentDocument{
		listingFields:               m.toListingFields(),
		LivingSpace:                 formatFloat(m.LivingSpace),
		PlotArea:                    formatFloat(m.PlotArea),
		NumberOfRooms:               formatFloat(m.NumberOfRooms),
		Courtage:                    m.toCourtage(),
		BuildingType:                m.BuildingType.ValueString(),
		buildingFields:              m.toBuildingFields(),
		BaseRent:                    formatFloat(m.BaseRent),
		TotalRent:                   formatFloat(m.TotalRent),
		ServiceCharge:               formatFloat(m.ServiceCharge),
		Deposit:                     m.Deposit.ValueString(),
		HeatingCosts:                formatFloat(m.HeatingCosts),
		HeatingCostsInServiceCharge: m.HeatingCostsInServiceCharge.ValueString(),
		PetsAllowed:                 m.PetsAllowed.ValueString(),
		BuiltInKitchen:              m.BuiltInKitchen.ValueBoolPointer(),
	}
}

// toModel maps a GET response onto the model; see readListing for prior.
func (d *houseRentDocument) toModel(id string, prior *houseRentModel) (*houseRentModel, error) {
	p := &parser{}
	m := &houseRentModel{
		listingModel:                readListing(p, id, &d.listingFields, d.Courtage, &d.buildingFields, &prior.listingModel),
		LivingSpace:                 p.float64("livingSpace", d.LivingSpace, prior.LivingSpace),
		PlotArea:                    p.float64("plotArea", d.PlotArea, prior.PlotArea),
		NumberOfRooms:               p.float64("numberOfRooms", d.NumberOfRooms, prior.NumberOfRooms),
		BuildingType:                optionalString(d.BuildingType),
		BaseRent:                    p.float64("baseRent", d.BaseRent, prior.BaseRent),
		TotalRent:                   p.float64("totalRent", d.TotalRent, prior.TotalRent),
		ServiceCharge:               p.float64("serviceCharge", d.ServiceCharge, prior.ServiceCharge),
		Deposit:                     optionalString(d.Deposit),
		HeatingCosts:                p.float64("heatingCosts", d.HeatingCosts, prior.HeatingCosts),
		HeatingCostsInServiceCharge: optionalString(d.HeatingCostsInServiceCharge),
		PetsAllowed:                 optionalString(d.PetsAllowed),
		BuiltInKitchen:              types.BoolPointerValue(d.BuiltInKitchen),
	}
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}
