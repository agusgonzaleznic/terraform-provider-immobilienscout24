resource "immobilienscout24_realestate" "example" {
  title            = "My Apartment"
  description_note = "Created with Terraform."
  street           = "Teststrasse"
  house_number     = "42"
  postcode         = "12345"
  city             = "Berlin"
}
