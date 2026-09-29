# terraform-provider-immobilienscout24

An unofficial Terraform and OpenTofu provider for [ImmobilienScout24](https://www.immobilienscout24.de/), built on the
[ImmobilienScout24 Import/Export API](https://api.immobilienscout24.de/api-docs/import-export/introduction/) and the
Terraform Plugin Framework.

> **Status:** early. It manages apartment rentals, their publication and their contact addresses. It follows the
> API documentation, the live XSD, and publish and contact calls observed on the ImmobilienScout24 sandbox. Its
> live acceptance test, which creates, publishes, updates, imports and destroys a listing, passes against the
> sandbox; the contact steps added to it since have not been run there yet. It has not been used against the
> production API.

## Resources

| Resource | Manages |
|----------|---------|
| [`immobilienscout24_apartment_rent`](docs/resources/apartment_rent.md) | An apartment for rent: create, read, update, delete, import |
| [`immobilienscout24_contact`](docs/resources/contact.md) | A contact address that listings show: create, read, update, delete, import |
| [`immobilienscout24_publication`](docs/resources/publication.md) | The publication of a listing on one publish channel: create, read, delete, import |

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) 1.14 or newer, or
  [OpenTofu](https://opentofu.org/docs/intro/install/) 1.11 or newer
- [Go](https://go.dev/doc/install) 1.25 or newer, to build from source
- ImmobilienScout24 API credentials (OAuth 1.0a): a consumer key and secret, plus an access token and
  secret, for example your personal access token. See
  [Get Your Client Credentials](https://api.immobilienscout24.de/api-docs/get-started/get-your-client-credentials/).
  Keys are issued per environment (sandbox or production).

## Installation

The provider is not on a registry yet, so build it locally and point Terraform at the build.

1. Build the binary into your Go bin directory:

   ```sh
   git clone https://github.com/agusgonzaleznic/terraform-provider-immobilienscout24.git
   cd terraform-provider-immobilienscout24
   go install .
   ```

2. Tell Terraform to use that build instead of a registry download. Add this to `~/.terraformrc`,
   replacing the path with the output of `go env GOBIN` (or `$(go env GOPATH)/bin` when that is empty):

   ```hcl
   provider_installation {
     dev_overrides {
       "agusgonzaleznic/immobilienscout24" = "/Users/you/go/bin"
     }
     direct {}
   }
   ```

   With `dev_overrides` in place, skip `terraform init` and run `terraform plan` directly.

## Configuration

Pass the credentials through environment variables so they never appear in your Terraform files:

| Provider attribute    | Environment variable                    |
|-----------------------|-----------------------------------------|
| `consumer_key`        | `IMMOBILIENSCOUT24_CONSUMER_KEY`        |
| `consumer_secret`     | `IMMOBILIENSCOUT24_CONSUMER_SECRET`     |
| `access_token`        | `IMMOBILIENSCOUT24_ACCESS_TOKEN`        |
| `access_token_secret` | `IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET` |

A value set in the provider block wins over the environment variable. All four are sensitive.

`environment` selects the API: `sandbox` (the default, `https://rest.sandbox-immobilienscout24.de/restapi/api`)
or `production` (`https://rest.immobilienscout24.de/restapi/api`). `base_url` replaces the API root entirely;
it exists for tests and advanced use, and conflicts with `environment`.

## Example

```hcl
terraform {
  required_providers {
    immobilienscout24 = {
      source = "agusgonzaleznic/immobilienscout24"
    }
  }
}

provider "immobilienscout24" {
  environment = "sandbox"
}

resource "immobilienscout24_apartment_rent" "example" {
  title        = "Bright two-room apartment near Nordbahnhof"
  show_address = true

  address = {
    street       = "Invalidenstrasse"
    house_number = "65"
    postcode     = "10557"
    city         = "Berlin"
  }

  base_rent       = 950
  living_space    = 62.5
  number_of_rooms = 2

  courtage = {
    has_courtage = "NO"
  }
}
```

Every attribute is described in [docs/resources/apartment_rent.md](docs/resources/apartment_rent.md). An existing
listing can be imported by its scout object id:

```sh
terraform import immobilienscout24_apartment_rent.example 315000001
```

## Publishing

New listings are created unpublished. An `immobilienscout24_publication` publishes a listing on one publish
channel, and destroying it unpublishes the listing on that channel only:

```hcl
resource "immobilienscout24_publication" "portal" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  channel_id     = "10000" # ImmobilienScout24 (www.immobilienscout24.de)
}

resource "immobilienscout24_publication" "homepage" {
  real_estate_id = immobilienscout24_apartment_rent.example.id
  channel_id     = "10001" # the realtor's own homepage
}
```

> **Warning:** in production, publishing on channel `10000` uses the paid contingent of your ImmobilienScout24
> account, and without contingent the API answers `No contingent available`. Try a configuration on the sandbox
> first (the default `environment`), where no bookings take place.

- Publishing on `10000` activates the listing. Unpublishing it there deactivates the listing, even while it stays
  published on `10001`.
- Changing `real_estate_id` or `channel_id` unpublishes and publishes anew. Deleting a listing also removes its
  publications.
- If a listing is already published on the channel, creating the publication fails with the command that imports
  it. A publication's id is `{real_estate_id}_{channel_id}`:

  ```sh
  terraform import immobilienscout24_publication.portal 315000001_10000
  ```

- The API documentation asks for publish requests one after the other, so the provider sends them one at a time,
  even when Terraform creates several publications in parallel.

## Contacts

A listing shows one contact address. An `immobilienscout24_contact` manages one, and `contact_id` points a listing
at it:

```hcl
resource "immobilienscout24_contact" "leasing" {
  email        = "vermietung@example.com"
  lastname     = "Beispiel"
  phone_number = "+49 30 24301999"
}

resource "immobilienscout24_apartment_rent" "example" {
  # ...
  contact_id = immobilienscout24_contact.leasing.id
}
```

- Phone numbers are written in one piece: country code, area code and subscriber number, separated by spaces,
  such as `+49 30 24301999`. The country code starts with `+`, not `00`, and after `+49` the area code has no
  leading `0`.
- Every account has exactly one **default contact**, which a listing without a contact gets. Setting
  `default_contact = true` makes a contact the default, and the previous default loses the flag. Setting
  `default_contact = false` is rejected, because ImmobilienScout24 ignores it: the default only moves when another
  contact becomes the default. Leave the attribute out to keep the current flag. Set `default_contact = true` on one
  contact only: when two contacts both set it, the apply fails with "Another contact became the default contact".
- ImmobilienScout24 refuses to delete the default contact. To destroy it, first make another contact the default,
  with `default_contact = true` on another `immobilienscout24_contact` or on the website. Moving the default to a
  new contact and removing the old one in the same apply works: the provider waits up to 15 seconds for the new
  contact to take the default before it gives up deleting the old one.
- Destroying a contact moves the listings that use it to the default contact.
- An update replaces the whole contact, so the fields the resource does not model (`company`, `officeHours`,
  `portraitUrl`, `clickOutUrl`, `localPartnerContact`, `businessCardContact`) are reset.
- An existing contact can be imported by its id:

  ```sh
  terraform import immobilienscout24_contact.leasing 124309506
  ```

**Fixed: updates no longer reset a listing's contact.** Up to v0.1.0, every update of an
`immobilienscout24_apartment_rent` reset the listing to the account's default contact. ImmobilienScout24 does that
when an update leaves the contact out, and the provider never sent one, so a contact chosen on the website was lost
on the next `terraform apply` that changed the listing. The resource now sends the listing's contact on every
update: the configured `contact_id`, or, without one, the contact the listing already has.

## Limitations

- **Tested on the sandbox, not on production.** Production API access is paid, so the provider has only been
  run against the sandbox; see [Development](#development).
- **Publish requests wait for each other within one provider configuration.** Two provider configurations (for
  example aliases) for the same account can still publish in parallel.
- **Apartment rentals only.** Other real estate types (houses, apartments for sale, commercial) are not supported.
  Importing an object of another type fails with an error rather than misreading it.
- **Partial field coverage.** The resource models the mandatory fields and the common optional ones. Updates are
  full replacements on the API side, so fields it does not model (energy certificate, attachments metadata, and so
  on) may be reset when Terraform updates a listing that was edited elsewhere. The listing's contact is kept.
- **Delete is a hard delete.** ImmobilienScout24 recommends deactivating listings instead of deleting them;
  `terraform destroy` deletes.
- No automatic retry on rate limiting (HTTP 429). The sandbox allows at most 200 write calls per minute.

## Development

```sh
make test      # unit tests, no network
make testacc   # acceptance tests against the local fake API, with Terraform
TF_ACC_TERRAFORM_PATH=$(which tofu) TF_ACC_PROVIDER_NAMESPACE=hashicorp TF_ACC_PROVIDER_HOST=registry.opentofu.org \
  make testacc # the same with OpenTofu
make lint
make generate  # regenerate docs/ from the schema and examples/
```

The acceptance tests start an in-process fake of the API that checks the OAuth signature, the documented headers,
and the element order of every request body against the live XSD in `immobilienscout24/testdata/` (for contacts,
against the order of the documented examples, which that XSD predates), and that fails publish requests which
overlap. It mirrors the contact behaviour observed on the sandbox, including the default contact rules. The tests
need a `terraform` or `tofu` binary but no credentials.

The live sandbox test runs only when asked for explicitly. It creates a contact and an apartment that shows it,
publishes the apartment on channel `10000` of the sandbox, updates it and destroys all three. It never changes the
account's default contact:

```sh
TF_ACC=1 IMMOBILIENSCOUT24_LIVE=1 \
  IMMOBILIENSCOUT24_CONSUMER_KEY=... IMMOBILIENSCOUT24_CONSUMER_SECRET=... \
  IMMOBILIENSCOUT24_ACCESS_TOKEN=... IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET=... \
  go test ./immobilienscout24 -run TestAccApartmentRent_liveSandbox -v
```

## License

MIT
