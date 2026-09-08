default: fmt test build

build:
	go build -v ./...

generate:
	go tool oapi-codegen --config openapi/oapi-codegen.yaml openapi/kaneo.openapi.json
	go tool oapi-codegen --config openapi/oapi-codegen.admin.yaml openapi/kaneo.admin.openapi.yaml

generate-docs:
	go tool tfplugindocs generate --provider-name kaneo --rendered-provider-name Kaneo

validate-docs:
	go tool tfplugindocs validate --provider-name kaneo

fmt:
	gofmt -s -w .
	terraform fmt -recursive examples/

fmt-check:
	test -z "$$(gofmt -s -l .)"
	terraform fmt -check -recursive examples/

lint:
	go tool golangci-lint run ./...

test:
	go test -v -cover ./...

# Override KANEO_VERSION to test another release; Docker and Terraform are required.
testacc:
	TF_ACC=1 go test -count=1 -v -timeout 15m -artifacts -outputdir="$(CURDIR)" ./internal/provider -run '^TestAcc'

.PHONY: default build generate generate-docs validate-docs fmt fmt-check lint test testacc
