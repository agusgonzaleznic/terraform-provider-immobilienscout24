package immobilienscout24

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Enumerations, copied from the simple types of the live XSD.
var (
	apartmentTypes = []string{"ROOF_STOREY", "LOFT", "MAISONETTE", "PENTHOUSE", "TERRACED_FLAT", "GROUND_FLOOR",
		"APARTMENT", "RAISED_GROUND_FLOOR", "HALF_BASEMENT", "OTHER", "NO_INFORMATION"} // common:ApartmentType
	yesNotApplicable   = []string{"YES", "NOT_APPLICABLE"}                     // common:YesNotApplicableType
	yesNoNotApplicable = []string{"YES", "NO", "NOT_APPLICABLE"}               // common:YesNoNotApplicableType
	petsAllowedValues  = []string{"NO_INFORMATION", "NEGOTIABLE", "YES", "NO"} // common:PetsAllowedType
)

// Limits from the field table on the Import/Export Introduction page.
const (
	maxPrice         = 9999999999999.99
	maxArea          = 99999999.99
	maxTextNoteBytes = 3999
)

var nonEmpty = stringvalidator.LengthAtLeast(1)

func quotedEnum(values []string) string {
	return "`" + strings.Join(values, "`, `") + "`"
}

func optionalStringWithMax(desc string, maxRunes int) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc,
		Optional:            true,
		Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(maxRunes)},
	}
}

func textNote(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc + " At most 3999 bytes. The API supports no HTML except `<br>`.",
		Optional:            true,
		Validators:          []validator.String{nonEmpty, stringvalidator.LengthAtMost(maxTextNoteBytes)},
	}
}

func price(desc string) schema.Float64Attribute {
	return schema.Float64Attribute{
		MarkdownDescription: desc + " In EUR.",
		Optional:            true,
		Validators:          []validator.Float64{float64validator.Between(0, maxPrice)},
	}
}

func enum(desc string, values []string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc + " One of " + quotedEnum(values) + ".",
		Optional:            true,
		Validators:          []validator.String{stringvalidator.OneOf(values...)},
	}
}

func optionalBool(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{MarkdownDescription: desc, Optional: true}
}

// apartmentRentSchema is the schema of immobilienscout24_apartment_rent. Each
// description names the XSD element the attribute maps to; see
// apartment_rent_xml.go for the wire order.
func apartmentRentSchema() schema.Schema {
	return schema.Schema{
		MarkdownDescription: "An apartment for rent (`realestates:apartmentRent`) in the ImmobilienScout24 account " +
			"of the access token. New objects are created unpublished; publishing is not managed by this provider yet.\n\n" +
			"The API treats every update as a full replacement (\"You have to send all attributes, also if only one " +
			"attribute has changed\"). Fields this resource does not model, such as an energy certificate or a contact " +
			"set on the website, can therefore be reset when Terraform updates the object.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The scout object id that ImmobilienScout24 assigned on creation.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"external_id": schema.StringAttribute{
				MarkdownDescription: "Your own id for the object (`externalId`), unique within the account, at most 50 " +
					"characters. When omitted, ImmobilienScout24 sets it to the scout object id. Removing it from the " +
					"configuration later keeps the current value.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:    []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(50)},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Title of the listing (`title`), at most 100 characters.",
				Required:            true,
				Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(100)},
			},
			"address": schema.SingleNestedAttribute{
				MarkdownDescription: "Address of the apartment (`address`).",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"street": schema.StringAttribute{
						MarkdownDescription: "Street (`street`), at most 100 characters.",
						Required:            true,
						Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(100)},
					},
					"house_number": schema.StringAttribute{
						MarkdownDescription: "House number (`houseNumber`), at most 10 characters.",
						Required:            true,
						Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(10)},
					},
					"postcode": schema.StringAttribute{
						MarkdownDescription: "Postcode (`postcode`), at most 5 characters.",
						Required:            true,
						Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(5)},
					},
					"city": schema.StringAttribute{
						MarkdownDescription: "City (`city`), at most 50 characters.",
						Required:            true,
						Validators:          []validator.String{nonEmpty, stringvalidator.UTF8LengthAtMost(50)},
					},
					"coordinates": schema.SingleNestedAttribute{
						MarkdownDescription: "WGS84 coordinates (`wgs84Coordinate`). ImmobilienScout24 asks for these only " +
							"when it cannot geocode the address, and to omit them otherwise.",
						Optional: true,
						Attributes: map[string]schema.Attribute{
							"latitude": schema.Float64Attribute{
								MarkdownDescription: "Latitude, between -90 and 90.",
								Required:            true,
								Validators:          []validator.Float64{float64validator.Between(-90, 90)},
							},
							"longitude": schema.Float64Attribute{
								MarkdownDescription: "Longitude, between -180 and 180.",
								Required:            true,
								Validators:          []validator.Float64{float64validator.Between(-180, 180)},
							},
						},
					},
				},
			},
			"show_address": schema.BoolAttribute{
				MarkdownDescription: "Whether the listing shows the full address (`showAddress`).",
				Required:            true,
			},
			"description_note": textNote("Object description (`descriptionNote`)."),
			"furnishing_note":  textNote("Description of the furnishing (`furnishingNote`)."),
			"location_note":    textNote("Description of the location (`locationNote`)."),
			"other_note":       textNote("Other information (`otherNote`)."),
			"apartment_type":   enum("Apartment type (`apartmentType`).", apartmentTypes),
			"floor": schema.Int64Attribute{
				MarkdownDescription: "Floor the apartment is on (`floor`), 0 to 999.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 999)},
			},
			"lift":      optionalBool("Whether the building has a lift (`lift`)."),
			"cellar":    enum("Cellar (`cellar`).", yesNotApplicable),
			"free_from": optionalStringWithMax("When the apartment is available, as free text (`freeFrom`), at most 50 characters.", 50),
			"number_of_floors": schema.Int64Attribute{
				MarkdownDescription: "Number of floors of the building (`numberOfFloors`), 0 to 999.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 999)},
			},
			"base_rent": schema.Float64Attribute{
				MarkdownDescription: "Monthly base rent (`baseRent`, Kaltmiete) in EUR. Must be above 0: the API only accepts 0 (price on request) for plots and commercial types.",
				Required:            true,
				Validators:          []validator.Float64{float64validator.Between(0, maxPrice), float64validator.NoneOf(0)},
			},
			"total_rent":     price("Monthly total rent (`totalRent`, Warmmiete)."),
			"service_charge": price("Monthly service charge (`serviceCharge`, Nebenkosten)."),
			"deposit":        optionalStringWithMax("Deposit, as free text (`deposit`), at most 50 characters.", 50),
			"heating_costs":  price("Monthly heating costs (`heatingCosts`)."),
			"heating_costs_in_service_charge": enum("Whether the heating costs are included in the service charge "+
				"(`heatingCostsInServiceCharge`). Must not be `NOT_APPLICABLE` when `heating_costs` is set.", yesNoNotApplicable),
			"pets_allowed": enum("Whether pets are allowed (`petsAllowed`).", petsAllowedValues),
			"living_space": schema.Float64Attribute{
				MarkdownDescription: "Living space in square metres (`livingSpace`).",
				Required:            true,
				Validators:          []validator.Float64{float64validator.Between(0, maxArea)},
			},
			"number_of_rooms": schema.Float64Attribute{
				MarkdownDescription: "Number of rooms (`numberOfRooms`), 1 to 999.99. Half rooms are allowed, for example `2.5`.",
				Required:            true,
				Validators:          []validator.Float64{float64validator.Between(1, 999.99)},
			},
			"built_in_kitchen": optionalBool("Whether there is a built-in kitchen (`builtInKitchen`)."),
			"balcony":          optionalBool("Whether there is a balcony (`balcony`)."),
			"garden":           optionalBool("Whether there is a garden (`garden`)."),
			"courtage": schema.SingleNestedAttribute{
				MarkdownDescription: "Broker commission (`courtage`).",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"has_courtage": schema.StringAttribute{
						MarkdownDescription: "Whether a commission is charged (`hasCourtage`). One of " + quotedEnum(yesNoNotApplicable) + ".",
						Required:            true,
						Validators:          []validator.String{stringvalidator.OneOf(yesNoNotApplicable...)},
					},
					"courtage": optionalStringWithMax("The commission, as free text such as `2,38 Monatsmieten` "+
						"(`courtage`), at most 100 characters. Required when `has_courtage` is `YES`.", 100),
					"courtage_note": optionalStringWithMax("Note on the commission (`courtageNote`), at most 500 characters.", 500),
				},
			},
		},
	}
}
