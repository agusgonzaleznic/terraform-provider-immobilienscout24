package immobilienscout24

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// houseBuyKind is immobilienscout24_house_buy, a realestates:houseBuy.
var houseBuyKind = &listingKind[houseBuyModel, houseBuyDocument]{
	typeName:       "_house_buy",
	realEstateType: realEstateType{root: "houseBuy", plural: "houses for sale"},
	noun:           "house for sale",
	schema:         houseBuySchema,
	listing:        (*houseBuyModel).listing,
	toDocument:     (*houseBuyModel).toDocument,
	toModel:        (*houseBuyDocument).toModel,
}

// NewHouseBuyResource returns the immobilienscout24_house_buy resource.
func NewHouseBuyResource() resource.Resource {
	return &listingResource[houseBuyModel, houseBuyDocument]{kind: houseBuyKind}
}

func houseBuySchema() schema.Schema {
	return listingSchema("A house for sale (`realestates:houseBuy`)", "house",
		houseAttributes(), saleAttributes("house"), roomAttributes())
}

type houseBuyModel struct {
	listingModel
	BuildingType  types.String  `tfsdk:"building_type"`
	Rented        types.String  `tfsdk:"rented"`
	PurchasePrice types.Float64 `tfsdk:"purchase_price"`
	LivingSpace   types.Float64 `tfsdk:"living_space"`
	PlotArea      types.Float64 `tfsdk:"plot_area"`
	NumberOfRooms types.Float64 `tfsdk:"number_of_rooms"`
}

// houseBuyDocument is a realestates:houseBuy; see listingFields for the field
// order. buildingType is required, and numberOfRooms is an xs:string in this
// type.
type houseBuyDocument struct {
	listingFields                   // 1 to 19
	BuildingType   string           `xml:"buildingType,omitempty"` // 22 HouseBuy
	buildingFields                  // 23 to 40
	Rented         string           `xml:"rented,omitempty"` // 46
	Price          *priceElement    `xml:"price"`            // 51
	LivingSpace    *string          `xml:"livingSpace"`      // 52
	PlotArea       *string          `xml:"plotArea"`         // 53
	NumberOfRooms  *string          `xml:"numberOfRooms"`    // 54
	Courtage       *courtageElement `xml:"courtage"`         // 56
}

// toDocument builds the complete request document from a plan; see
// toListingFields.
func (m *houseBuyModel) toDocument() *houseBuyDocument {
	return &houseBuyDocument{
		listingFields:  m.toListingFields(),
		BuildingType:   m.BuildingType.ValueString(),
		buildingFields: m.toBuildingFields(),
		Rented:         m.Rented.ValueString(),
		Price:          toPrice(m.PurchasePrice),
		LivingSpace:    formatFloat(m.LivingSpace),
		PlotArea:       formatFloat(m.PlotArea),
		NumberOfRooms:  formatFloat(m.NumberOfRooms),
		Courtage:       m.toCourtage(),
	}
}

// toModel maps a GET response onto the model; see readListing for prior.
func (d *houseBuyDocument) toModel(id string, prior *houseBuyModel) (*houseBuyModel, error) {
	p := &parser{}
	m := &houseBuyModel{
		listingModel:  readListing(p, id, &d.listingFields, d.Courtage, &d.buildingFields, &prior.listingModel),
		BuildingType:  optionalString(d.BuildingType),
		Rented:        optionalString(d.Rented),
		PurchasePrice: p.float64("price.value", d.Price.value(), prior.PurchasePrice),
		LivingSpace:   p.float64("livingSpace", d.LivingSpace, prior.LivingSpace),
		PlotArea:      p.float64("plotArea", d.PlotArea, prior.PlotArea),
		NumberOfRooms: p.float64("numberOfRooms", d.NumberOfRooms, prior.NumberOfRooms),
	}
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}
