# The first picture uploaded becomes the title picture of the listing.
# title_picture = true moves the flag to this picture instead.
resource "immobilienscout24_attachment_picture" "living_room" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  file           = "${path.module}/files/living-room.jpg"
  title          = "Living room"
  title_picture  = true
}

resource "immobilienscout24_attachment_picture" "kitchen" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  file           = "${path.module}/files/kitchen.jpg"
  title          = "Kitchen"
  external_id    = "berlin-mitte-kitchen"
}
