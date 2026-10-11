BINARY  := hankopod
PKG     := ./cmd/hankopod
DIST    := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Static, reproducible binaries: no cgo, no local paths.
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

LINUX   := linux/amd64 linux/arm64
TERMUX  := android/arm64
WINDOWS := windows/amd64 windows/arm64
OSX     := darwin/amd64 darwin/arm64

.DEFAULT_GOAL := build

.PHONY: help build install test vet linux termux windows osx macos all dist checksums clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: ## Build for this machine into bin/
	$(GOBUILD) -o bin/$(BINARY)$(shell go env GOEXE) $(PKG)

install: ## Install into $GOBIN (or ~/go/bin)
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test: vet ## Run vet and tests
	go test ./...

vet:
	go vet ./...

linux: $(addprefix $(DIST)/,$(subst /,-,$(LINUX))) ## Linux amd64 + arm64

termux: $(DIST)/android-arm64 ## Android/Termux arm64

windows: $(addprefix $(DIST)/,$(subst /,-,$(WINDOWS))) ## Windows amd64 + arm64

osx: $(addprefix $(DIST)/,$(subst /,-,$(OSX))) ## macOS Intel + Apple silicon

macos: osx

all: linux termux windows osx ## Every platform into dist/

dist: checksums ## Every platform plus SHA256SUMS

checksums: all ## Write dist/SHA256SUMS
	cd $(DIST) && { command -v sha256sum >/dev/null && sha256sum $(BINARY)-* || shasum -a 256 $(BINARY)-*; } > SHA256SUMS

# dist/<GOOS>-<GOARCH> builds dist/hankopod-<name>-<arch>[.exe], where name is
# the GOOS except android -> termux (where it runs) and darwin -> macos.
$(DIST)/%:
	@os=$(word 1,$(subst -, ,$*)); arch=$(word 2,$(subst -, ,$*)); \
	name=$$os; [ $$os = android ] && name=termux; [ $$os = darwin ] && name=macos; \
	ext=; [ $$os = windows ] && ext=.exe; \
	out=$(DIST)/$(BINARY)-$$name-$$arch$$ext; \
	mkdir -p $(DIST); echo "build $$out"; \
	GOOS=$$os GOARCH=$$arch $(GOBUILD) -o $$out $(PKG)

clean: ## Remove build output
	rm -rf bin $(DIST)
