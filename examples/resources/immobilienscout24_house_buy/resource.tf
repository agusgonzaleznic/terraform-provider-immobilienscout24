resource "immobilienscout24_house_buy" "example" {
  external_id   = "berlin-mitte-doppelhaus"
  title         = "Semi-detached house near Nordbahnhof"
  show_address  = true
  building_type = "SEMIDETACHED_HOUSE"
  contact_id    = immobilienscout24_contact.example.id

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  description_note = "Built in 1935, roof renewed in 2019."
  number_of_floors = 3
  cellar           = "YES"

  purchase_price  = 650000
  living_space    = 160
  plot_area       = 450
  number_of_rooms = 6

  # A consumption certificate from before 1 May 2014, whose value includes
  # hot water.
  energy_certificate = {
    availability  = "AVAILABLE"
    creation_date = "BEFORE_01_MAY_2014"
  }
  construction_year                      = 1935
  heating_type                           = "GAS_HEATING"
  energy_sources                         = ["GAS"]
  building_energy_rating_type            = "ENERGY_CONSUMPTION"
  thermal_characteristic                 = 120.5
  energy_consumption_contains_warm_water = "YES"

  courtage = {
    has_courtage = "YES"
    courtage     = "3,57 % inkl. MwSt."
  }
}
