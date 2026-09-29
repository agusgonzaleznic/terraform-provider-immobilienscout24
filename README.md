# terraform-provider-immobilienscout24

An unofficial Terraform and OpenTofu provider for [ImmobilienScout24](https://www.immobilienscout24.de/), built on the
[ImmobilienScout24 Import/Export API](https://api.immobilienscout24.de/api-docs/import-export/introduction/) and the
Terraform Plugin Framework.

> **Status:** early. It manages one resource, `immobilienscout24_apartment_rent`, with create, read, update,
> delete and import. The implementation follows the API documentation and the live XSD, and is tested
> against a local fake of the API. It has **not been run against the real sandbox yet**, and it is not
> published to a registry.

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

## Limitations

- **Verified against the documentation, not the live API.** The request format, paths, headers and responses
  follow the official docs and the live XSD, and a local fake enforces them in the tests. A live test against the
  sandbox exists but has not been run yet; see [Development](#development).
- **No publishing.** New listings are created unpublished, and the provider cannot publish or unpublish them yet.
- **Apartment rentals only.** Other real estate types (houses, apartments for sale, commercial) are not supported.
  Importing an object of another type fails with an error rather than misreading it.
- **Partial field coverage.** The resource models the mandatory fields and the common optional ones. Updates are
  full replacements on the API side, so fields it does not model (energy certificate, contact, attachments metadata,
  and so on) may be reset when Terraform updates a listing that was edited elsewhere.
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
and the element order of every request body against the live XSD in `immobilienscout24/testdata/`. They need a
`terraform` or `tofu` binary but no credentials.

The live sandbox test runs only when asked for explicitly:

```sh
TF_ACC=1 IMMOBILIENSCOUT24_LIVE=1 \
  IMMOBILIENSCOUT24_CONSUMER_KEY=... IMMOBILIENSCOUT24_CONSUMER_SECRET=... \
  IMMOBILIENSCOUT24_ACCESS_TOKEN=... IMMOBILIENSCOUT24_ACCESS_TOKEN_SECRET=... \
  go test ./immobilienscout24 -run TestAccApartmentRent_liveSandbox -v
```

## License

MIT
