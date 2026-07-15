# lanchat — ephemeral encrypted LAN terminal chat
#
#   make            build ./lanchat for this machine
#   make install    build and copy to a directory on your PATH
#   make test       run the unit tests
#   make vet        run go vet
#   make fmt        format the source with gofmt
#   make cross      build binaries for macOS/Windows/Linux into dist/
#   make run        build and run (joins the "lobby")
#   make clean      remove build artifacts

BINARY  := lanchat
PKG     := ./cmd/lanchat
DIST    := dist
VERSION := $(shell awk -F'"' '/const version/{print $$2}' cmd/lanchat/main.go 2>/dev/null)
LDFLAGS := -s -w

.PHONY: all build install test vet fmt cross run clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

# Delegates to scripts/install.sh, which picks a directory already on your PATH
# (so `lanchat` runs from anywhere with no setup) or configures your PATH for you.
install:
	@sh "$(CURDIR)/scripts/install.sh"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Cross-compile static single-file binaries for every common desktop target.
cross:
	@mkdir -p $(DIST)
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-macos-arm64 $(PKG)
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-macos-amd64 $(PKG)
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-linux-amd64 $(PKG)
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-linux-arm64 $(PKG)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-windows-amd64.exe $(PKG)
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-windows-arm64.exe $(PKG)
	@echo "built $(VERSION) into $(DIST)/:" && ls -1 $(DIST)

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
