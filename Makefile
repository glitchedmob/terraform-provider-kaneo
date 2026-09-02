default: fmt test build

build:
	go build -v ./...

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

.PHONY: default build fmt fmt-check lint test
