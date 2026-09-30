package immobilienscout24

// The typed calls for apartment rentals that the tests written before the
// other listing types use. The resources, and the newer tests, call
// CreateRealEstate, GetRealEstate, UpdateRealEstate and the kind's mapping.

import "context"

func (c *Client) CreateApartmentRent(ctx context.Context, doc *apartmentRentDocument) (string, error) {
	return c.CreateRealEstate(ctx, &apartmentRentKind.realEstateType, doc)
}

func (c *Client) GetApartmentRent(ctx context.Context, id string) (*apartmentRentDocument, error) {
	doc := &apartmentRentDocument{}
	if err := c.GetRealEstate(ctx, &apartmentRentKind.realEstateType, id, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (c *Client) UpdateApartmentRent(ctx context.Context, id string, doc *apartmentRentDocument) error {
	return c.UpdateRealEstate(ctx, &apartmentRentKind.realEstateType, id, doc)
}

func marshalApartmentRent(doc *apartmentRentDocument) ([]byte, error) {
	return marshalListing(&apartmentRentKind.realEstateType, doc)
}

func fromDocument(id string, doc *apartmentRentDocument, prior *apartmentRentModel) (*apartmentRentModel, error) {
	return doc.toModel(id, prior)
}
