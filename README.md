# terraform-provider-immobilienscout24

An unofficial Terraform provider for [ImmobilienScout24](https://www.immobilienscout24.de/), built on the
[ImmobilienScout24 API](https://api.immobilienscout24.de/api-docs/import-export/introduction/) and the
Terraform Plugin Framework.

> **Status:** early prototype. It supports one resource, `immobilienscout24_realestate`, and only
> creates it, against the sandbox API. It is not published to a registry yet and is not ready for
> production use.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) 1.0 or newer
- [Go](https://go.dev/doc/install) 1.24 or newer, to build from source
- ImmobilienScout24 sandbox API credentials (OAuth 1.0a). See
  [Get Your Client Credentials](https://api.immobilienscout24.de/api-docs/get-started/get-your-client-credentials/).

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
  consumer_key        = var.consumer_key
  consumer_secret     = var.consumer_secret
  access_token        = var.access_token
  access_token_secret = var.access_token_secret
}

resource "immobilienscout24_realestate" "example" {
  title            = "My Apartment"
  description_note = "Created with Terraform."
  street           = "Teststrasse"
  house_number     = "42"
  postcode         = "12345"
  city             = "Berlin"
}
```

Keep the credentials out of the configuration file: pass them as variables from a secret store,
never as literals.

## Limitations

- Only create is implemented. Read, update and delete are not, so `terraform destroy` does not
  remove the listing from ImmobilienScout24.
- There is no test coverage yet.

## License

MIT
