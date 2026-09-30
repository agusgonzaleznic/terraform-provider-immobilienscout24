package immobilienscout24

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Enumerations, copied from the simple types of the live XSD.
var (
	apartmentTypes = []string{"ROOF_STOREY", "LOFT", "MAISONETTE", "PENTHOUSE", "TERRACED_FLAT", "GROUND_FLOOR",
		"APARTMENT", "RAISED_GROUND_FLOOR", "HALF_BASEMENT", "OTHER", "NO_INFORMATION"} // common:ApartmentType
	buildingTypes = []string{"NO_INFORMATION", "SINGLE_FAMILY_HOUSE", "MID_TERRACE_HOUSE", "END_TERRACE_HOUSE",
		"MULTI_FAMILY_HOUSE", "BUNGALOW", "FARMHOUSE", "SEMIDETACHED_HOUSE", "VILLA", "CASTLE_MANOR_HOUSE",
		"SPECIAL_REAL_ESTATE", "TERRACE_HOUSE", "OTHER"} // common:BuildingType
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
		MarkdownDescription: desc + " In EUR, at most two decimal places.",
		Optional:            true,
		Validators:          []validator.Float64{float64validator.Between(0, maxPrice), twoDecimals},
	}
}

// The API fills these fields in when they are left out, so the schema carries
// the same defaults. Without them every create would end with Terraform
// reporting an inconsistent result (seen on the sandbox, 2026-09-29).
func enumWithDefault(desc string, values []string, def string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc + " One of " + quotedEnum(values) + ". Defaults to `" + def + "`, as the API does.",
		Optional:            true,
		Computed:            true,
		Default:             stringdefault.StaticString(def),
		Validators:          []validator.String{stringvalidator.OneOf(values...)},
	}
}

// enumWithSentDefault is enumWithDefault for an element the API requires: the
// default is the provider's, sent because the element cannot be left out.
func enumWithSentDefault(desc string, values []string, def string) schema.StringAttribute {
	a := enumWithDefault(desc, values, def)
	a.MarkdownDescription = desc + " One of " + quotedEnum(values) + ". Defaults to `" + def + "`."
	return a
}

func boolDefaultFalse(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: desc + " Defaults to `false`, as the API does.",
		Optional:            true,
		Computed:            true,
		Default:             booldefault.StaticBool(false),
	}
}

// listingSchema is the schema of a listing resource: the attributes every
// listing has, then those of the type in own. what starts the description,
// such as "An apartment for rent (`realestates:apartmentRent`)", and noun is
// what the listing is, such as "apartment". Each attribute's description
// names the XSD element it maps to; the type's document gives the wire order.
// An attribute defined twice is a bug in the provider, so it panics.
func listingSchema(what, noun string, own ...map[string]schema.Attribute) schema.Schema {
	attrs := listingAttributes(noun)
	for _, m := range append([]map[string]schema.Attribute{energyAttributes()}, own...) {
		for name, a := range m {
			if _, ok := attrs[name]; ok {
				panic(fmt.Sprintf("listing schema: attribute %q is defined twice", name))
			}
			attrs[name] = a
		}
	}
	return schema.Schema{
		MarkdownDescription: what + " in the ImmobilienScout24 account of the access token. New objects are created " +
			"unpublished; publish them with `immobilienscout24_publication`.\n\n" +
			"The API treats every update as a full replacement (\"You have to send all attributes, also if only one " +
			"attribute has changed\"). Fields this resource does not model, such as the condition or the number of " +
			"bedrooms, can therefore be reset when Terraform updates the object. The contact is not: every update " +
			"sends the listing's contact, see `contact_id`.",
		Attributes: attrs,
	}
}

// listingAttributes are the attributes every listing resource has, apart from
// the energy attributes.
func listingAttributes(noun string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
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
			MarkdownDescription: "Address of the " + noun + " (`address`).",
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
		"contact_id": schema.StringAttribute{
			MarkdownDescription: "The id of the contact the listing shows (`contact`), for example " +
				"`immobilienscout24_contact.example.id`. Changing it updates the listing in place. When omitted, a new " +
				"listing gets the account's default contact, and later the listing keeps whatever contact it has, " +
				"also one chosen on the website: the API resets a listing to the default contact when an update " +
				"leaves the contact out, so every update first reads the listing's current contact and sends it. " +
				"Deleting a contact moves its listings to the default contact.",
			Optional: true,
			Computed: true,
			Validators: []validator.String{
				stringvalidator.RegexMatches(positiveID, "must be a whole number in digits, without a leading zero"),
			},
		},
		"description_note": textNote("Object description (`descriptionNote`)."),
		"furnishing_note":  textNote("Description of the furnishing (`furnishingNote`)."),
		"location_note":    textNote("Description of the location (`locationNote`)."),
		"other_note":       textNote("Other information (`otherNote`)."),
		"cellar":           enumWithDefault("Cellar (`cellar`).", yesNotApplicable, "NOT_APPLICABLE"),
		"free_from":        optionalStringWithMax("When the "+noun+" is available, as free text (`freeFrom`), at most 50 characters.", 50),
		"number_of_floors": schema.Int64Attribute{
			MarkdownDescription: "Number of floors of the building (`numberOfFloors`), 0 to 999.",
			Optional:            true,
			Validators:          []validator.Int64{int64validator.Between(0, 999)},
		},
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
	}
}

// apartmentAttributes are the attributes of both apartment types.
func apartmentAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"apartment_type": enumWithDefault("Apartment type (`apartmentType`).", apartmentTypes, "NO_INFORMATION"),
		"floor": schema.Int64Attribute{
			MarkdownDescription: "Floor the apartment is on (`floor`), 0 to 999.",
			Optional:            true,
			Validators:          []validator.Int64{int64validator.Between(0, 999)},
		},
		"lift":             boolDefaultFalse("Whether the building has a lift (`lift`)."),
		"built_in_kitchen": builtInKitchenAttribute(),
		"balcony":          boolDefaultFalse("Whether there is a balcony (`balcony`)."),
		"garden":           boolDefaultFalse("Whether there is a garden (`garden`)."),
	}
}

// rentAttributes are the attributes of both types for rent.
func rentAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"base_rent": schema.Float64Attribute{
			MarkdownDescription: "Monthly base rent (`baseRent`, Kaltmiete) in EUR, at most two decimal places. Must be " +
				"above 0: the API only accepts 0 (price on request) for plots and commercial types.",
			Required: true,
			Validators: []validator.Float64{float64validator.Between(0, maxPrice), float64validator.NoneOf(0),
				twoDecimals},
		},
		"total_rent":     price("Monthly total rent (`totalRent`, Warmmiete)."),
		"service_charge": price("Monthly service charge (`serviceCharge`, Nebenkosten)."),
		"deposit":        optionalStringWithMax("Deposit, as free text (`deposit`), at most 50 characters.", 50),
		"heating_costs":  price("Monthly heating costs (`heatingCosts`)."),
		"heating_costs_in_service_charge": enumWithDefault("Whether the heating costs are included in the service charge "+
			"(`heatingCostsInServiceCharge`). Must be `YES` or `NO` when `heating_costs` is set.", yesNoNotApplicable, "NOT_APPLICABLE"),
		"pets_allowed": enumWithDefault("Whether pets are allowed (`petsAllowed`).", petsAllowedValues, "NO_INFORMATION"),
	}
}

// saleAttributes are the attributes of both types for sale; noun is what is
// for sale, such as "apartment".
func saleAttributes(noun string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"purchase_price": schema.Float64Attribute{
			MarkdownDescription: "Purchase price (`price`, Kaufpreis) in EUR, at most two decimal places. `0` is " +
				"accepted (observed on the sandbox, 2026-09-30), and the field table says that such a listing shows " +
				"\"Preis auf Anfrage\" (price on request).",
			Required:   true,
			Validators: []validator.Float64{float64validator.Between(0, maxPrice), twoDecimals},
		},
		"rented": enumWithDefault("Whether the "+noun+" is rented out (`rented`).", yesNotApplicable, "NOT_APPLICABLE"),
	}
}

// houseAttributes are the attributes of both house types.
func houseAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"building_type": enumWithSentDefault("House type (`buildingType`). ImmobilienScout24 refuses a house without it, "+
			"so the provider always sends one; the API keeps a `NO_INFORMATION` sent this way (observed 2026-09-30).",
			buildingTypes, "NO_INFORMATION"),
		"plot_area": schema.Float64Attribute{
			MarkdownDescription: "Plot area in square metres (`plotArea`), at most two decimal places.",
			Required:            true,
			Validators:          []validator.Float64{float64validator.Between(0, maxArea), twoDecimals},
		},
	}
}

// roomAttributes are the living space and the rooms, which every listing type
// has at positions of its own.
func roomAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"living_space": schema.Float64Attribute{
			MarkdownDescription: "Living space in square metres (`livingSpace`), at most two decimal places.",
			Required:            true,
			Validators:          []validator.Float64{float64validator.Between(0, maxArea), twoDecimals},
		},
		"number_of_rooms": schema.Float64Attribute{
			MarkdownDescription: "Number of rooms (`numberOfRooms`), 1 to 999.99 with at most two decimal places. Half " +
				"rooms are allowed, for example `2.5`.",
			Required:   true,
			Validators: []validator.Float64{float64validator.Between(1, 999.99), twoDecimals},
		},
	}
}

func builtInKitchenAttribute() schema.BoolAttribute {
	return boolDefaultFalse("Whether there is a built-in kitchen (`builtInKitchen`).")
}

// twoDecimals rejects a number with more than two decimal places as the
// provider sends it. ImmobilienScout24 rounds decimals to two places
// (livingSpace 50.555 came back as 50.56, observed 2026-09-30), so a third
// place would end every apply in an inconsistent result.
var twoDecimals = atMostTwoDecimals{}

type atMostTwoDecimals struct{}

func (atMostTwoDecimals) Description(_ context.Context) string {
	return "value must have at most two decimal places"
}

func (v atMostTwoDecimals) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (atMostTwoDecimals) ValidateFloat64(_ context.Context, req validator.Float64Request, resp *validator.Float64Response) {
	sent := formatFloat(req.ConfigValue)
	if sent == nil {
		return
	}
	if _, decimals, _ := strings.Cut(*sent, "."); len(decimals) > 2 {
		resp.Diagnostics.AddAttributeError(req.Path, "Too many decimal places",
			fmt.Sprintf("%s has more than two decimal places. ImmobilienScout24 rounds decimals to two places, so "+
				"the number would come back as another one.", *sent))
	}
}
