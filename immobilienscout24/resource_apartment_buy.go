package immobilienscout24

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// apartmentBuyKind is immobilienscout24_apartment_buy, a
// realestates:apartmentBuy.
var apartmentBuyKind = &listingKind[apartmentBuyModel, apartmentBuyDocument]{
	typeName:       "_apartment_buy",
	realEstateType: realEstateType{root: "apartmentBuy", plural: "apartments for sale"},
	noun:           "apartment for sale",
	schema:         apartmentBuySchema,
	listing:        (*apartmentBuyModel).listing,
	toDocument:     (*apartmentBuyModel).toDocument,
	toModel:        (*apartmentBuyDocument).toModel,
}

// NewApartmentBuyResource returns the immobilienscout24_apartment_buy resource.
func NewApartmentBuyResource() resource.Resource {
	return &listingResource[apartmentBuyModel, apartmentBuyDocument]{kind: apartmentBuyKind}
}

func apartmentBuySchema() schema.Schema {
	return listingSchema("An apartment for sale (`realestates:apartmentBuy`)", "apartment",
		apartmentAttributes(), saleAttributes("apartment"), roomAttributes(), map[string]schema.Attribute{
			"service_charge": price("Monthly service charge of the owners' association (`serviceCharge`, Hausgeld)."),
		})
}

type apartmentBuyModel struct {
	listingModel
	ApartmentType  types.String  `tfsdk:"apartment_type"`
	Floor          types.Int64   `tfsdk:"floor"`
	Lift           types.Bool    `tfsdk:"lift"`
	Rented         types.String  `tfsdk:"rented"`
	PurchasePrice  types.Float64 `tfsdk:"purchase_price"`
	LivingSpace    types.Float64 `tfsdk:"living_space"`
	NumberOfRooms  types.Float64 `tfsdk:"number_of_rooms"`
	BuiltInKitchen types.Bool    `tfsdk:"built_in_kitchen"`
	Balcony        types.Bool    `tfsdk:"balcony"`
	Garden         types.Bool    `tfsdk:"garden"`
	ServiceCharge  types.Float64 `tfsdk:"service_charge"`
}

// apartmentBuyDocument is a realestates:apartmentBuy; see listingFields for
// the field order.
type apartmentBuyDocument struct {
	listingFields                   // 1 to 19
	ApartmentType  string           `xml:"apartmentType,omitempty"` // 20 ApartmentBuy
	Floor          *string          `xml:"floor"`                   // 21
	Lift           *bool            `xml:"lift"`                    // 22
	buildingFields                  // 24 to 41
	Rented         string           `xml:"rented,omitempty"` // 47
	Price          *priceElement    `xml:"price"`            // 52
	LivingSpace    *string          `xml:"livingSpace"`      // 53
	NumberOfRooms  *string          `xml:"numberOfRooms"`    // 54
	BuiltInKitchen *bool            `xml:"builtInKitchen"`   // 56
	Balcony        *bool            `xml:"balcony"`          // 57
	Garden         *bool            `xml:"garden"`           // 59
	Courtage       *courtageElement `xml:"courtage"`         // 60
	ServiceCharge  *string          `xml:"serviceCharge"`    // 61
}

// toDocument builds the complete request document from a plan; see
// toListingFields.
func (m *apartmentBuyModel) toDocument() *apartmentBuyDocument {
	return &apartmentBuyDocument{
		listingFields:  m.toListingFields(),
		ApartmentType:  m.ApartmentType.ValueString(),
		Floor:          formatInt(m.Floor),
		Lift:           m.Lift.ValueBoolPointer(),
		buildingFields: m.toBuildingFields(),
		Rented:         m.Rented.ValueString(),
		Price:          toPrice(m.PurchasePrice),
		LivingSpace:    formatFloat(m.LivingSpace),
		NumberOfRooms:  formatFloat(m.NumberOfRooms),
		BuiltInKitchen: m.BuiltInKitchen.ValueBoolPointer(),
		Balcony:        m.Balcony.ValueBoolPointer(),
		Garden:         m.Garden.ValueBoolPointer(),
		Courtage:       m.toCourtage(),
		ServiceCharge:  formatFloat(m.ServiceCharge),
	}
}

// toModel maps a GET response onto the model; see readListing for prior.
func (d *apartmentBuyDocument) toModel(id string, prior *apartmentBuyModel) (*apartmentBuyModel, error) {
	p := &parser{}
	m := &apartmentBuyModel{
		listingModel:   readListing(p, id, &d.listingFields, d.Courtage, &d.buildingFields, &prior.listingModel),
		ApartmentType:  optionalString(d.ApartmentType),
		Floor:          p.int64("floor", d.Floor),
		Lift:           types.BoolPointerValue(d.Lift),
		Rented:         optionalString(d.Rented),
		PurchasePrice:  p.float64("price.value", d.Price.value(), prior.PurchasePrice),
		LivingSpace:    p.float64("livingSpace", d.LivingSpace, prior.LivingSpace),
		NumberOfRooms:  p.float64("numberOfRooms", d.NumberOfRooms, prior.NumberOfRooms),
		BuiltInKitchen: types.BoolPointerValue(d.BuiltInKitchen),
		Balcony:        types.BoolPointerValue(d.Balcony),
		Garden:         types.BoolPointerValue(d.Garden),
		ServiceCharge:  p.float64("serviceCharge", d.ServiceCharge, prior.ServiceCharge),
	}
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}
