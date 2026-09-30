resource "immobilienscout24_apartment_buy" "example" {
  external_id  = "berlin-mitte-2og-rechts"
  title        = "Two-room apartment with balcony near Nordbahnhof"
  show_address = true
  contact_id   = immobilienscout24_contact.example.id

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  description_note = "Quiet courtyard side, renovated in 2024."
  apartment_type   = "APARTMENT"
  floor            = 2
  number_of_floors = 5
  lift             = true
  balcony          = true
  built_in_kitchen = true
  cellar           = "YES"
  free_from        = "01.11.2026"

  purchase_price  = 389000
  service_charge  = 310
  living_space    = 62.5
  number_of_rooms = 2

  energy_certificate = {
    availability     = "AVAILABLE"
    creation_date    = "FROM_01_MAY_2014"
    efficiency_class = "C"
  }
  construction_year           = 1998
  heating_type                = "CENTRAL_HEATING"
  energy_sources              = ["GAS"]
  building_energy_rating_type = "ENERGY_CONSUMPTION"
  thermal_characteristic      = 95.5

  courtage = {
    has_courtage = "YES"
    courtage     = "3,57 % inkl. MwSt."
  }
}
