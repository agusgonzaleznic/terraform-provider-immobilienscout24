package immobilienscout24

// The energy attributes that every listing type has, and the elements of
// buildingFields: the energy certificate and its values, plus cellar,
// free_from and number_of_floors, which sit between them in every XSD type.
// The sandbox handled the energy fields of all four types the same way
// (observed 2026-09-30).

import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Enumerations, copied from the simple types of the live XSD.
var (
	energyCertificateAvailabilities = []string{"AVAILABLE", "NOT_AVAILABLE_YET", "NOT_REQUIRED"}           // common:EnergyCertificateAvailability
	energyCertificateCreationDates  = []string{"NOT_APPLICABLE", "BEFORE_01_MAY_2014", "FROM_01_MAY_2014"} // common:EnergyCertificateCreationDate
	buildingEnergyRatingTypes       = []string{"NO_INFORMATION", "ENERGY_REQUIRED", "ENERGY_CONSUMPTION"}  // common:BuildingEnergyRatingType
	// common:HeatingTypeEnev2014 without NO_INFORMATION, which the API drops
	// instead of storing it (observed 2026-09-30).
	heatingTypes = []string{"SELF_CONTAINED_CENTRAL_HEATING", "STOVE_HEATING", "CENTRAL_HEATING",
		"COMBINED_HEAT_AND_POWER_PLANT", "ELECTRIC_HEATING", "DISTRICT_HEATING", "FLOOR_HEATING", "GAS_HEATING",
		"WOOD_PELLET_HEATING", "NIGHT_STORAGE_HEATER", "OIL_HEATING", "SOLAR_HEATING", "HEAT_PUMP"}
	// common:EnergySourceEnev2014. The API only takes the last eight, from
	// BIO_ENERGY on, with realEstateQuery; see client.go.
	energySources = []string{"NO_INFORMATION", "GEOTHERMAL", "SOLAR_HEATING", "PELLET_HEATING", "GAS", "OIL",
		"DISTRICT_HEATING", "ELECTRICITY", "COAL", "ACID_GAS", "SOUR_GAS", "LIQUID_GAS", "STEAM_DISTRICT_HEATING",
		"WOOD", "WOOD_CHIPS", "COAL_COKE", "LOCAL_HEATING", "HEAT_SUPPLY", "BIO_ENERGY", "WIND_ENERGY",
		"HYDRO_ENERGY", "ENVIRONMENTAL_THERMAL_ENERGY", "COMBINED_HEAT_AND_POWER_FOSSIL_FUELS",
		"COMBINED_HEAT_AND_POWER_RENEWABLE_ENERGY", "COMBINED_HEAT_AND_POWER_REGENERATIVE_ENERGY",
		"COMBINED_HEAT_AND_POWER_BIO_ENERGY"}
	// energyEfficiencyClass is an xs:string. These are the values the sandbox
	// takes; A_PLUS fails its schema check (observed 2026-09-30).
	energyEfficiencyClasses = []string{"NOT_APPLICABLE", "A+", "A", "B", "C", "D", "E", "F", "G", "H"}
)

// noEnergySource is the API's value for energy sources that are not given.
const noEnergySource = "NO_INFORMATION"

type energyCertificateModel struct {
	Availability    types.String `tfsdk:"availability"`
	CreationDate    types.String `tfsdk:"creation_date"`
	EfficiencyClass types.String `tfsdk:"efficiency_class"`
}

// energyAttributes are the energy attributes of every listing resource.
func energyAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"energy_certificate": schema.SingleNestedAttribute{
			MarkdownDescription: "Energy certificate (`energyCertificate`). ImmobilienScout24 adds fields of its own " +
				"to it, such as `legalConstructionYear` from `construction_year`; they are not managed.",
			Optional: true,
			Attributes: map[string]schema.Attribute{
				"availability": schema.StringAttribute{
					MarkdownDescription: "Whether there is an energy certificate (`energyCertificateAvailability`). One of " +
						quotedEnum(energyCertificateAvailabilities) + ". With `NOT_AVAILABLE_YET` or `NOT_REQUIRED`, " +
						"ImmobilienScout24 refuses `creation_date`, `efficiency_class`, `thermal_characteristic` and " +
						"`building_energy_rating_type`, whatever their value.",
					Required:   true,
					Validators: []validator.String{stringvalidator.OneOf(energyCertificateAvailabilities...)},
				},
				"creation_date": schema.StringAttribute{
					MarkdownDescription: "When the certificate was issued (`energyCertificateCreationDate`). One of " +
						quotedEnum(energyCertificateCreationDates) + ".",
					Optional:   true,
					Validators: []validator.String{stringvalidator.OneOf(energyCertificateCreationDates...)},
				},
				"efficiency_class": schema.StringAttribute{
					MarkdownDescription: "Energy efficiency class (`energyEfficiencyClass`). One of " +
						quotedEnum(energyEfficiencyClasses) + ". Any class, `NOT_APPLICABLE` included, needs " +
						"`creation_date` `FROM_01_MAY_2014` and `building_energy_rating_type` `ENERGY_REQUIRED` or " +
						"`ENERGY_CONSUMPTION`.",
					Optional:   true,
					Validators: []validator.String{stringvalidator.OneOf(energyEfficiencyClasses...)},
				},
			},
		},
		"construction_year": schema.Int64Attribute{
			MarkdownDescription: "Year of construction (`constructionYear`), 1000 to 9999.",
			Optional:            true,
			Validators:          []validator.Int64{int64validator.Between(1000, 9999)},
		},
		"heating_type": schema.StringAttribute{
			MarkdownDescription: "Heating type (`heatingTypeEnev2014`). One of " + quotedEnum(heatingTypes) + ". " +
				"ImmobilienScout24 derives the older `heatingType` from it, for some values only; that field is not managed.",
			Optional:   true,
			Validators: []validator.String{stringvalidator.OneOf(heatingTypes...)},
		},
		"energy_sources": schema.SetAttribute{
			MarkdownDescription: "The essential energy sources (`energySourcesEnev2014`), at least one of " +
				quotedEnum(energySources) + ". Defaults to `[\"" + noEnergySource + "\"]`, as the API does; `" +
				noEnergySource + "` cannot be combined with other sources. ImmobilienScout24 derives the older " +
				"`firingTypes` from them; that field is not managed.",
			ElementType: types.StringType,
			Optional:    true,
			Computed:    true,
			Default: setdefault.StaticValue(types.SetValueMust(types.StringType,
				[]attr.Value{types.StringValue(noEnergySource)})),
			Validators: []validator.Set{
				setvalidator.SizeAtLeast(1),
				setvalidator.ValueStringsAre(stringvalidator.OneOf(energySources...)),
				noEnergySourceAlone{},
			},
		},
		"building_energy_rating_type": schema.StringAttribute{
			MarkdownDescription: "What the energy certificate's value is (`buildingEnergyRatingType`): `ENERGY_REQUIRED` " +
				"(Endenergiebedarf), `ENERGY_CONSUMPTION` (Energieverbrauchskennwert) or `NO_INFORMATION`.",
			Optional:   true,
			Validators: []validator.String{stringvalidator.OneOf(buildingEnergyRatingTypes...)},
		},
		"thermal_characteristic": schema.Float64Attribute{
			MarkdownDescription: "The energy certificate's value (`thermalCharacteristic`) in kWh/(m²·a), 0.01 to " +
				"9999.99, at most two decimal places.",
			Optional:   true,
			Validators: []validator.Float64{float64validator.Between(0.01, 9999.99), twoDecimals},
		},
		"energy_consumption_contains_warm_water": enumWithDefault("Whether the energy consumption includes hot water "+
			"(`energyConsumptionContainsWarmWater`). `YES` needs `thermal_characteristic`, and when both "+
			"`energy_certificate.creation_date` and `building_energy_rating_type` are set, it is only valid for "+
			"`BEFORE_01_MAY_2014` with `ENERGY_CONSUMPTION`.", yesNotApplicable, "NOT_APPLICABLE"),
	}
}

// noEnergySourceAlone rejects NO_INFORMATION next to another energy source:
// the API drops it there (observed 2026-09-30), and the next plan would add
// it back.
type noEnergySourceAlone struct{}

func (noEnergySourceAlone) Description(_ context.Context) string {
	return noEnergySource + " must be the only energy source"
}

func (v noEnergySourceAlone) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (noEnergySourceAlone) ValidateSet(_ context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || len(req.ConfigValue.Elements()) < 2 {
		return
	}
	if slices.Contains(setStrings(req.ConfigValue), noEnergySource) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid energy sources",
			noEnergySource+" cannot be combined with other energy sources: ImmobilienScout24 drops it next to "+
				"another source. Leave it out, or use it on its own.")
	}
}

// setStrings returns the known elements of a set of strings, sorted.
func setStrings(set types.Set) []string {
	var out []string
	for _, v := range set.Elements() {
		if s, ok := v.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
			out = append(out, s.ValueString())
		}
	}
	slices.Sort(out)
	return out
}

// toBuildingFields builds the energy and building elements. The API keeps no
// order of the energy sources, so they are sent sorted.
func (m *listingModel) toBuildingFields() buildingFields {
	b := buildingFields{
		Cellar:                             m.Cellar.ValueString(),
		ConstructionYear:                   formatInt(m.ConstructionYear),
		FreeFrom:                           m.FreeFrom.ValueString(),
		HeatingTypeEnev2014:                m.HeatingType.ValueString(),
		BuildingEnergyRatingType:           m.BuildingEnergyRatingType.ValueString(),
		ThermalCharacteristic:              formatFloat(m.ThermalCharacteristic),
		EnergyConsumptionContainsWarmWater: m.EnergyConsumptionContainsWarmWater.ValueString(),
		NumberOfFloors:                     formatInt(m.NumberOfFloors),
	}
	if c := m.EnergyCertificate; c != nil {
		b.EnergyCertificate = &energyCertificateElement{
			Availability:    c.Availability.ValueString(),
			CreationDate:    c.CreationDate.ValueString(),
			EfficiencyClass: c.EfficiencyClass.ValueString(),
		}
	}
	if sources := setStrings(m.EnergySources); len(sources) > 0 {
		b.EnergySources = &energySourcesElement{Sources: sources}
	}
	return b
}

// readBuilding maps the energy and building elements onto m; see readListing
// for prior.
func readBuilding(p *parser, b *buildingFields, prior *listingModel, m *listingModel) {
	// A certificate without availability, which this resource cannot write,
	// reads as none.
	m.EnergyCertificate = nil
	if c := b.EnergyCertificate; c != nil && c.Availability != "" {
		m.EnergyCertificate = &energyCertificateModel{
			Availability:    types.StringValue(c.Availability),
			CreationDate:    optionalString(c.CreationDate),
			EfficiencyClass: optionalString(c.EfficiencyClass),
		}
	}
	m.Cellar = optionalString(b.Cellar)
	m.ConstructionYear = p.int64("constructionYear", b.ConstructionYear)
	m.FreeFrom = optionalString(b.FreeFrom)
	m.HeatingType = optionalString(b.HeatingTypeEnev2014)
	m.EnergySources = readEnergySources(b.EnergySources, prior.EnergySources)
	m.BuildingEnergyRatingType = optionalString(b.BuildingEnergyRatingType)
	m.ThermalCharacteristic = p.float64("thermalCharacteristic", b.ThermalCharacteristic, prior.ThermalCharacteristic)
	m.EnergyConsumptionContainsWarmWater = optionalString(b.EnergyConsumptionContainsWarmWater)
	m.NumberOfFloors = p.int64("numberOfFloors", b.NumberOfFloors)
}

// readEnergySources maps the returned energy sources onto a set. When it holds
// the same sources as the prior value, the prior value is kept: the API
// returns them in an order of its own (OIL, GAS, SOLAR_HEATING came back as
// SOLAR_HEATING, OIL, GAS; observed 2026-09-30).
func readEnergySources(e *energySourcesElement, prior types.Set) types.Set {
	if e == nil {
		return types.SetNull(types.StringType)
	}
	var values []attr.Value
	for _, s := range e.Sources {
		v := types.StringValue(s)
		if !slices.ContainsFunc(values, v.Equal) {
			values = append(values, v)
		}
	}
	set := types.SetValueMust(types.StringType, values)
	if !prior.IsNull() && !prior.IsUnknown() && prior.Equal(set) {
		return prior
	}
	return set
}

// Energy values the rules below compare against.
const (
	certificateFromMay2014   = "FROM_01_MAY_2014"
	certificateBeforeMay2014 = "BEFORE_01_MAY_2014"
	ratingRequired           = "ENERGY_REQUIRED"
	ratingConsumption        = "ENERGY_CONSUMPTION"
)

// validateEnergy enforces the rules of the energy attributes that the sandbox
// enforces with a 412 (observed 2026-09-30), so that the plan fails instead of
// the apply. A rule is skipped while a value it reads is unknown. The codes in
// the messages are the API's.
func validateEnergy(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) {
	certPath := path.Root("energy_certificate")
	var cert types.Object
	var rating, warmWater types.String
	var thermal types.Float64
	var d diag.Diagnostics
	d.Append(config.GetAttribute(ctx, certPath, &cert)...)
	d.Append(config.GetAttribute(ctx, path.Root("building_energy_rating_type"), &rating)...)
	d.Append(config.GetAttribute(ctx, path.Root("energy_consumption_contains_warm_water"), &warmWater)...)
	d.Append(config.GetAttribute(ctx, path.Root("thermal_characteristic"), &thermal)...)
	// The framework reads an attribute of an unknown object as null, so the
	// certificate's attributes are only read when the certificate is known.
	availability, date, class := types.StringNull(), types.StringNull(), types.StringNull()
	switch {
	case cert.IsUnknown():
		availability, date, class = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	case !cert.IsNull():
		d.Append(config.GetAttribute(ctx, certPath.AtName("availability"), &availability)...)
		d.Append(config.GetAttribute(ctx, certPath.AtName("creation_date"), &date)...)
		d.Append(config.GetAttribute(ctx, certPath.AtName("efficiency_class"), &class)...)
	}
	diags.Append(d...)
	if d.HasError() {
		return
	}
	rated := rating.ValueString() == ratingRequired || rating.ValueString() == ratingConsumption

	if known(class, date, rating) && !class.IsNull() && (date.ValueString() != certificateFromMay2014 || !rated) {
		diags.AddAttributeError(certPath.AtName("efficiency_class"), "Efficiency class not valid for this certificate",
			"An efficiency_class, NOT_APPLICABLE included, needs energy_certificate.creation_date = \""+
				certificateFromMay2014+"\" and building_energy_rating_type = \""+ratingRequired+"\" or \""+
				ratingConsumption+"\". ImmobilienScout24 refuses it otherwise "+
				"(EV_EFFICIENCY_CLASS_NOT_VALID_FOR_THIS_CERTIFICATE).")
	}
	warmPath := path.Root("energy_consumption_contains_warm_water")
	if known(warmWater, thermal) && warmWater.ValueString() == "YES" && thermal.IsNull() {
		diags.AddAttributeError(warmPath, "Warm water without a thermal characteristic",
			"energy_consumption_contains_warm_water = \"YES\" needs a thermal_characteristic. ImmobilienScout24 "+
				"refuses it otherwise (ENERGY_CONSUMPTION_CONTAINS_WARM_WATER_WITHOUT_ENERGY_CONSUMPTION).")
	}
	if known(warmWater, date, rating) && warmWater.ValueString() == "YES" &&
		((date.ValueString() == certificateFromMay2014 && rated) ||
			(date.ValueString() == certificateBeforeMay2014 && rating.ValueString() == ratingRequired)) {
		diags.AddAttributeError(warmPath, "Warm water not valid for this certificate",
			fmt.Sprintf("energy_consumption_contains_warm_water = \"YES\" is not valid with creation_date %q and "+
				"building_energy_rating_type %q. With both set, ImmobilienScout24 only accepts it for %q with %q "+
				"(EV_WARM_WATER_FIELD_NOT_VALID_FOR_THIS_CERTIFICATE).", date.ValueString(), rating.ValueString(),
				certificateBeforeMay2014, ratingConsumption))
	}
	// Any value counts, a creation date of NOT_APPLICABLE and a rating type of
	// NO_INFORMATION included (observed 2026-09-30 for both availabilities).
	if known(availability, date, class, thermal, rating) &&
		(availability.ValueString() == "NOT_AVAILABLE_YET" || availability.ValueString() == "NOT_REQUIRED") &&
		(!date.IsNull() || !class.IsNull() || !thermal.IsNull() || !rating.IsNull()) {
		diags.AddAttributeError(certPath.AtName("availability"), "Energy certificate not available but fields filled",
			fmt.Sprintf("With energy_certificate.availability %q, ImmobilienScout24 refuses a creation_date, an "+
				"efficiency_class, a thermal_characteristic and a building_energy_rating_type, whatever their value "+
				"(EV_NOT_AVAILABLE_BUT_FIELDS_FILLED). Leave them out, or set availability to \"AVAILABLE\".",
				availability.ValueString()))
	}
}

// known reports whether none of the values is unknown.
func known(values ...attr.Value) bool {
	return !slices.ContainsFunc(values, attr.Value.IsUnknown)
}
