PLUGIN_ID := onelegdave.omachat
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: helper test lint validate clean

helper:
	mkdir -p bin
	go build -mod=vendor -ldflags "-X main.version=$(VERSION)" -o bin/omachatd ./cmd/omachatd

test:
	go test -mod=vendor ./...

lint:
	go vet -mod=vendor ./...

validate:
	omarchy plugin validate .

clean:
	rm -rf bin
