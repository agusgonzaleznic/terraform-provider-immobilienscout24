resource "immobilienscout24_attachment_link" "video" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  url            = "https://www.example.com/videos/berlin-mitte"
  title          = "Video tour"
}
