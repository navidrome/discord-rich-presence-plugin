SHELL := /usr/bin/env bash
.PHONY: test build package clean

PLUGIN_NAME := discord-rich-presence
WASM_FILE := plugin.wasm

test:
	go test -race ./...

build:
	tinygo build -opt=2 -scheduler=none -no-debug -o $(WASM_FILE) -target wasi -buildmode=c-shared .

package: build
	zip $(PLUGIN_NAME).ndp $(WASM_FILE) manifest.json

clean:
	rm -f $(WASM_FILE) $(PLUGIN_NAME).ndp

release:
	@if [[ ! "${V}" =~ ^[0-9]+\.[0-9]+\.[0-9]+.*$$ ]]; then echo "Usage: make release V=X.X.X"; exit 1; fi
	gh workflow run create-release.yml -f version=${V}
	@echo "Release v${V} workflow triggered. Check progress: gh run list --workflow=create-release.yml"
.PHONY: release
