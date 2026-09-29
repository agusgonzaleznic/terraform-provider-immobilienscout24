# Publish the listing on ImmobilienScout24 itself. In production, this uses
# the paid contingent of the account.
resource "immobilienscout24_publication" "portal" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  channel_id     = "10000"
}

# Publish the same listing on the realtor's own homepage.
resource "immobilienscout24_publication" "homepage" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  channel_id     = "10001"
}
