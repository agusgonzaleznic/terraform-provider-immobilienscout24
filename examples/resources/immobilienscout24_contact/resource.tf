resource "immobilienscout24_contact" "example" {
  email      = "vermietung@example.com"
  salutation = "FEMALE"
  firstname  = "Anna"
  lastname   = "Beispiel"
  position   = "Leasing agent"

  phone_number = "+49 30 24301999"

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }
  country_code = "DEU"

  homepage_url = "https://www.example.com"
  external_id  = "leasing-anna"
}
