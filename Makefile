# Dictator — macOS build.
#   make            build dist/Dictator.app (universal: Apple silicon + Intel)
#   make install    copy it to /Applications
#   make run        build and run the bare binary in this terminal
#   make cert       one-time: create a local signing identity (keeps Accessibility across rebuilds)
#   make test       unit tests
#   make measure    memory / CPU / wakeups / size of the running app

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
APP     := dist/Dictator.app
GO      ?= go

export CGO_ENABLED := 1
export MACOSX_DEPLOYMENT_TARGET := 13.0
export CGO_CFLAGS := -O2 -mmacosx-version-min=13.0
export CGO_LDFLAGS := -mmacosx-version-min=13.0

LDFLAGS := -s -w -X main.version=$(VERSION)
# Sign with a stable identity when there is one: an ad-hoc signature changes on
# every build, and macOS then silently drops the Accessibility grant.
SIGN_ID ?= $(shell security find-identity -p codesigning 2>/dev/null | grep -q '"Dictator Local Signing"' && echo 'Dictator Local Signing' || echo -)
BUNDLE_ID := io.github.apurvtaneja.dictator

SOURCES := $(shell find cmd internal -name '*.go' -o -name '*.m' -o -name '*.h') go.mod

.PHONY: all app cert install run test measure clean

all: app

dist/dictator-arm64: $(SOURCES)
	GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ ./cmd/dictator

dist/dictator-amd64: $(SOURCES)
	GOARCH=amd64 CGO_CFLAGS="$(CGO_CFLAGS) -arch x86_64" CGO_LDFLAGS="$(CGO_LDFLAGS) -arch x86_64" \
		$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ ./cmd/dictator

app: dist/dictator-arm64 dist/dictator-amd64 packaging/Info.plist packaging/entitlements.plist packaging/dictator.icns
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	lipo -create -output $(APP)/Contents/MacOS/Dictator dist/dictator-arm64 dist/dictator-amd64
	sed 's/__VERSION__/$(VERSION)/g' packaging/Info.plist > $(APP)/Contents/Info.plist
	cp packaging/dictator.icns $(APP)/Contents/Resources/dictator.icns
	codesign --force --options runtime --entitlements packaging/entitlements.plist --sign '$(SIGN_ID)' $(APP)
	@if [ '$(SIGN_ID)' = - ]; then echo "warning: ad-hoc signed — Accessibility resets on every build (run 'make cert' once)"; fi
	@echo "Built $(APP) — $$(du -sh $(APP) | cut -f1)"

install: app
	-@/Applications/Dictator.app/Contents/MacOS/Dictator --stop >/dev/null 2>&1
	rm -rf /Applications/Dictator.app
	cp -R $(APP) /Applications/
	@# an ad-hoc build can't match the old grant: clear it so the next launch asks again
	-@if [ '$(SIGN_ID)' = - ]; then tccutil reset Accessibility $(BUNDLE_ID) >/dev/null; fi
	@echo "Installed /Applications/Dictator.app"

cert:
	@sh packaging/make-cert.sh

run:
	$(GO) run ./cmd/dictator $(ARGS)

test:
	$(GO) test ./...

measure:
	@sh packaging/measure.sh

clean:
	rm -rf dist
