resource "immobilienscout24_house_rent" "example" {
  external_id   = "berlin-mitte-reihenhaus"
  title         = "Family house with garden near Nordbahnhof"
  show_address  = false
  building_type = "MID_TERRACE_HOUSE"
  contact_id    = immobilienscout24_contact.example.id

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  description_note = "Three floors, south-facing garden."
  number_of_floors = 3
  cellar           = "YES"
  built_in_kitchen = true
  pets_allowed     = "NEGOTIABLE"
  free_from        = "01.12.2026"

  base_rent                       = 1800
  service_charge                  = 250
  heating_costs                   = 150
  heating_costs_in_service_charge = "NO"
  total_rent                      = 2200
  deposit                         = "3 Kaltmieten"

  living_space    = 120
  plot_area       = 180
  number_of_rooms = 5

  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "A"
  }
  construction_year           = 2015
  heating_type                = "HEAT_PUMP"
  energy_sources              = ["ELECTRICITY", "ENVIRONMENTAL_THERMAL_ENERGY"]
  building_energy_rating_type = "ENERGY_REQUIRED"
  thermal_characteristic      = 45.2

  courtage = {
    has_courtage = "NO"
  }
}
