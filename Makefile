PLUGIN_ID := onelegdave.omachat

# Build offline from vendor/ with the installed Go. go.mod sets the minimum
# version; an older Go fails instead of downloading a newer toolchain.
export GOTOOLCHAIN := local
export GOPROXY := off
export GOSUMDB := off

.PHONY: helper test test-ui test-ui-headless lint validate clean

# WhatsApp session storage uses github.com/mattn/go-sqlite3, which requires
# CGO and a C compiler (gcc or clang). Google-only code does not need CGO,
# but this helper is one binary. Do not auto-install a toolchain.
helper:
	@if [ -z "$$CC" ] && ! command -v gcc >/dev/null 2>&1 && ! command -v clang >/dev/null 2>&1; then \
		echo "helper build needs a C compiler (gcc or clang) for CGO sqlite3"; \
		exit 1; \
	fi
	python3 scripts/updates.py build

test:
	go test -mod=vendor ./...
	go test -mod=vendor go.mau.fi/mautrix-gmessages/pkg/libgm
	go test -mod=vendor go.mau.fi/mautrix-meta/pkg/messagix go.mau.fi/mautrix-meta/pkg/messagix/httpclient go.mau.fi/mautrix-meta/pkg/messagix/dgw

test-ui:
	node --test tests/model*.test.cjs
	python3 -m unittest discover -s tests -p '*_test.py'
	python3 tests/run-qml.py

# Fixtures with isolated settings/data and D-Bus, plus a fresh unpaired helper.
# Full Wayland panel and helper lifecycle acceptance still uses test-ui.
test-ui-headless:
	node --test tests/model*.test.cjs
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p '*_test.py'
	OMACHAT_QML_HEADLESS=1 dbus-run-session -- python3 tests/run-qml.py

lint:
	go vet -mod=vendor ./...

validate:
	omarchy plugin validate .

clean:
	rm -rf bin
