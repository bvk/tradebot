# Copyright (c) 2025 BVK Chaitanya

export GO ?= go1.24.0
export GOBIN ?= $(CURDIR)
export PATH := $(PATH):$(HOME)/go/bin
export GOTESTFLAGS ?=

.PHONY: all
all: go-all go-test go-test-long;

.PHONY: clean
clean:
	git clean -f -X

.PHONY: check
check: all
	$(MAKE) go-test

.PHONY: go-all
go-all: go-generate
	GOOS=linux GOARCH=amd64 $(GO) build .
	GOOS=darwin GOARCH=arm64 $(GO) build -o tradebot.mac .

.PHONY: go-generate
go-generate:
	$(GO) generate ./...

.PHONY: go-test
go-test: go-all
	$(GO) test -fullpath -count=1 -coverprofile=coverage.out -short $(GOTESTFLAGS) ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: go-test-long
go-test-long: go-all
	$(GO) test -fullpath -failfast -count=1 -coverprofile=coverage.out $(GOTESTFLAGS) ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
