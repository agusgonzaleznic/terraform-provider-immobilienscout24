resource "immobilienscout24_apartment_rent" "example" {
  external_id  = "berlin-mitte-3og-links"
  title        = "Bright two-room apartment near Nordbahnhof"
  show_address = true

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  description_note = "Quiet courtyard side, renovated in 2024."
  apartment_type   = "APARTMENT"
  floor            = 3
  number_of_floors = 5
  lift             = true
  balcony          = true
  built_in_kitchen = true
  cellar           = "YES"
  pets_allowed     = "NEGOTIABLE"
  free_from        = "01.11.2026"

  base_rent                       = 950
  service_charge                  = 180
  heating_costs                   = 70
  heating_costs_in_service_charge = "NO"
  total_rent                      = 1200
  deposit                         = "3 Kaltmieten"

  living_space    = 62.5
  number_of_rooms = 2

  courtage = {
    has_courtage = "NO"
  }
}
