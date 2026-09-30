resource "immobilienscout24_attachment_pdf" "floor_plan" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  file           = "${path.module}/files/floor-plan.pdf"
  title          = "Floor plan"
  floorplan      = true
}
