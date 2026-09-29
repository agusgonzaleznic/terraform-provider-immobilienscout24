TFPLUGINDOCS_VERSION := v0.25.0
GOLANGCI_LINT_VERSION := v2.14.0

default: fmt lint build generate

build:
	go build -v ./...

install: build
	go install -v .

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

fmt:
	gofmt -s -w -e .
	terraform fmt -recursive examples/

generate:
	terraform fmt -recursive examples/
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name immobilienscout24

test:
	go test -v -cover -timeout=120s ./...

testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: default build install lint fmt generate test testacc
