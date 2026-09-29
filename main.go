package main

import (
	"context"
	"flag"
	"log"

	"github.com/agusgonzaleznic/terraform-provider-immobilienscout24/immobilienscout24"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is set by GoReleaser at build time.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), immobilienscout24.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/agusgonzaleznic/immobilienscout24",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
