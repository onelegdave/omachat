PLUGIN_ID := onelegdave.omachat
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: helper test test-ui lint validate clean

# WhatsApp session storage uses github.com/mattn/go-sqlite3, which requires
# CGO and a C compiler (gcc or clang). Google-only code does not need CGO,
# but this helper is one binary. Do not auto-install a toolchain.
helper:
	@if [ -z "$$CC" ] && ! command -v gcc >/dev/null 2>&1 && ! command -v clang >/dev/null 2>&1; then \
		echo "helper build needs a C compiler (gcc or clang) for CGO sqlite3"; \
		exit 1; \
	fi
	mkdir -p bin
	CGO_ENABLED=1 go build -mod=vendor -ldflags "-X main.version=$(VERSION)" -o bin/omachatd ./cmd/omachatd

test:
	go test -mod=vendor ./...
	go test -mod=vendor go.mau.fi/mautrix-gmessages/pkg/libgm

test-ui:
	node --test tests/model*.test.cjs
	python3 -m unittest discover -s tests -p '*_test.py'
	python3 tests/run-qml.py

lint:
	go vet -mod=vendor ./...

validate:
	omarchy plugin validate .

clean:
	rm -rf bin
