# Import an existing picture by {real_estate_id}/{attachment_id}. The API does
# not return the file, so the configuration names it; the next apply uploads it
# again unless its SHA-256 is the picture's externalCheckSum.
terraform import immobilienscout24_attachment_picture.living_room 315000001/904864036
