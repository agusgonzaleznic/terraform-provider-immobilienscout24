# terraform-provider-immobilienscout24
[![GitHub Release Workflow Status](https://img.shields.io/github/actions/workflow/status/agusgonzaleznic/terraform-provider-immobilienscout24/release.yml?label=Build&labelColor=black&logo=GitHub%20Actions&style=flat-square)](https://github.com/agusgonzaleznic/terraform-provider-immobilienscout24/actions/workflows/release.yml)
[![Terraform Registry Version](https://img.shields.io/github/v/release/agusgonzaleznic/terraform-provider-immobilienscout24?labelColor=black&label=TF%20Registry&logo=terraform&logoColor=7b42bc&color=7b42bc&style=flat-square)](https://registry.terraform.io/providers/agusgonzaleznic/immobilienscout24/latest)
[![Terraform Registry Downloads](https://img.shields.io/badge/dynamic/json?color=7b42bc&label=Downloads&labelColor=black&logo=terraform&logoColor=7b42bc&query=data.attributes.total&url=https%3A%2F%2Fregistry.terraform.io%2Fv2%2Fproviders%2F3133%2Fdownloads%2Fsummary&style=flat-square)](https://registry.terraform.io/providers/agusgonzaleznic/immobilienscout24/latest)

A minimal, unofficial Terraform provider for [Immobilienscout24](https://www.immobilienscout24.de/) that allows you to insert new real estate listings using the [Immobilienscout24 API](https://api.immobilienscout24.de/api-docs/introduction/).

> **Status:** MVP/prototype. Supports only "Insert Real Estate" on the sandbox API.  
> Not for production use.

---

## Features

- Insert new real estate entries into your Immobilienscout24 account (sandbox or production).
- Authenticate using OAuth1.

---

## Requirements

- [Terraform 1.12go+](https://www.terraform.io/downloads)
- Go 1.24+ (if building from source)
- Immobilienscout24 API credentials (OAuth1, sandbox or live)

---

## Usage

### 1. Build the Provider

```sh
git clone https://github.com/agusgonzaleznic/terraform-provider-immobilienscout24.git
cd terraform-provider-immobilienscout24
go build -o terraform-provider-immobilienscout24
````

Copy the resulting binary to your Terraform working directory or follow [the local provider installation instructions](https://developer.hashicorp.com/terraform/plugins/discovery#plugin-installation-directories).

---

### 2. Example `main.tf`

```hcl
terraform {
  required_providers {
    immobilienscout24 = {
      source  = "agusgonzaleznic/immobilienscout24"
      version = "0.1.0"
    }
  }
}

provider "immobilienscout24" {
  consumer_key        = "YOUR_CONSUMER_KEY"
  consumer_secret     = "YOUR_CONSUMER_SECRET"
  access_token        = "YOUR_ACCESS_TOKEN"
  access_token_secret = "YOUR_ACCESS_TOKEN_SECRET"
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

---

### 3. Initialize & Apply

```sh
terraform init
terraform apply
```

---

## Supported Resource(s)

* `immobilienscout24_realestate`

  * `title` (string, required)
  * `description_note` (string, required)
  * `street` (string, required)
  * `house_number` (string, required)
  * `postcode` (string, required)
  * `city` (string, required)
  * `id` (computed, assigned by API)

---

## Limitations

* Only supports creating real estate objects (insert).
* No read/update/delete or data sources.
* No test coverage or production hardening.
* Uses Immobilienscout24's [Import/Export API](https://api.immobilienscout24.de/api-docs/import-export/real-estate/insert-real-estate/).

---

## Development

PRs and issues are welcome!
This provider is for demonstration and prototyping only.

---

## License

MIT


