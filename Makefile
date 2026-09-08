GOLANGCI_LINT_VERSION := v2.13.2
GOLANGCI_LINT := $(CURDIR)/.bin/golangci-lint/$(GOLANGCI_LINT_VERSION)/golangci-lint

default: fmt test build

build:
	go build -v ./...

generate:
	go tool oapi-codegen --config openapi/oapi-codegen.yaml openapi/kaneo.openapi.json
	go tool oapi-codegen --config openapi/oapi-codegen.admin.yaml openapi/kaneo.admin.openapi.yaml

fmt:
	gofmt -s -w .
	terraform fmt -recursive examples/

fmt-check:
	test -z "$$(gofmt -s -l .)"
	terraform fmt -check -recursive examples/

$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b $(dir $(GOLANGCI_LINT)) $(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

test:
	go test -v -cover ./...

# Override KANEO_VERSION to test another release; Docker and Terraform are required.
testacc:
	TF_ACC=1 go test -count=1 -v -timeout 15m -artifacts -outputdir="$(CURDIR)" ./internal/provider -run '^TestAcc'

.PHONY: default build generate fmt fmt-check lint test testacc
