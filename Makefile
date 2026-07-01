.PHONY: fmt fmt-check test vet build check

GO_FILES := $(shell find . -name '*.go' -not -path './.git/*')

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	test -z "$$(gofmt -l $(GO_FILES))"

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./...

check: fmt-check test vet build
