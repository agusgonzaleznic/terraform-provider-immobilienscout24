package main

import (
	"context"

	"github.com/agusgonzaleznic/terraform-provider-immobilienscout24/immobilienscout24"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	providerserver.Serve(context.Background(), immobilienscout24.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/agusgonzaleznic/immobilienscout24",
	})
}
