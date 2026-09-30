# Import an existing PDF document by {real_estate_id}/{attachment_id}. The API
# does not return the file, so the configuration names it; the next apply
# uploads it again unless its SHA-256 is the document's externalCheckSum.
terraform import immobilienscout24_attachment_pdf.floor_plan 315000001/611952315
