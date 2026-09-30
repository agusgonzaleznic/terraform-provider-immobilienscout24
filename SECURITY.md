# Security

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/agusgonzaleznic/terraform-provider-immobilienscout24/security/advisories/new),
not in a public issue. Include the provider version and the steps to reproduce.

## Supported versions

Security fixes go into the latest release.

## Verifying a release

Every release is signed with the GPG key `131CD2FD69BA525373F222FD51BEA00CE6BEAD92`, which the Terraform Registry
and the OpenTofu Registry check on `terraform init` and `tofu init`. The key signs the release's `SHA256SUMS`, which
lists the checksum of every archive.
