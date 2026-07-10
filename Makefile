# model-fallback-chain — CLIProxyAPI plugin

UNAME_S := $(shell uname -s)

ifeq ($(OS),Windows_NT)
	PLUGIN_EXT := dll
else ifeq ($(UNAME_S),Darwin)
	PLUGIN_EXT := dylib
else
	PLUGIN_EXT := so
endif

PLUGIN_NAME := model-fallback-chain
OUTPUT := $(CURDIR)/bin/$(PLUGIN_NAME).$(PLUGIN_EXT)

.PHONY: build clean test fmt

build: $(OUTPUT)

$(OUTPUT): $(wildcard *.go) go.mod go.sum | bin
	go build -buildmode=c-shared -o $(abspath $@) .
	@rm -f bin/$(PLUGIN_NAME).h
	@echo "Built: $@"

bin:
	mkdir -p bin

clean:
	rm -rf bin

test:
	go test -v ./...

fmt:
	gofmt -w *.go
