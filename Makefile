default: fmt test build

build:
	go build -v ./...

generate:
	go tool oapi-codegen --config openapi/oapi-codegen.yaml openapi/kaneo.openapi.json

fmt:
	gofmt -s -w .
	terraform fmt -recursive examples/

fmt-check:
	test -z "$$(gofmt -s -l .)"
	terraform fmt -check -recursive examples/

lint:
	go vet ./...

test:
	go test -v -cover ./...

# Override KANEO_VERSION to test another release; Docker and Terraform are required.
testacc:
	bash integration/test.sh

.PHONY: default build generate fmt fmt-check lint test testacc
