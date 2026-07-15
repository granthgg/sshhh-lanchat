# lanchat — ephemeral encrypted LAN terminal chat
#
#   make            build ./lanchat for this machine
#   make install    build and copy to a directory on your PATH
#   make test       run the unit tests
#   make cross      build binaries for macOS/Windows/Linux into dist/
#   make run        build and run (joins the "lobby")
#   make clean      remove build artifacts

BINARY := lanchat
DIST   := dist
VERSION := $(shell awk -F'"' '/const version/{print $$2}' main.go 2>/dev/null)
LDFLAGS := -s -w

# Pick an install dir that is likely on PATH and writable.
PREFIX ?= $(shell if [ -w /usr/local/bin ]; then echo /usr/local/bin; else echo $$HOME/.local/bin; fi)

.PHONY: all build install test vet cross run clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: build
	mkdir -p "$(PREFIX)"
	install -m 0755 $(BINARY) "$(PREFIX)/$(BINARY)"
	@echo "installed $(PREFIX)/$(BINARY)"
	@case ":$$PATH:" in *":$(PREFIX):"*) ;; *) echo "note: add $(PREFIX) to your PATH";; esac

test:
	go test ./...

vet:
	go vet ./...

# Cross-compile static single-file binaries for every common desktop target.
cross:
	@mkdir -p $(DIST)
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-macos-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-macos-amd64 .
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-linux-arm64 .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lanchat-windows-arm64.exe .
	@echo "built $(VERSION) into $(DIST)/:" && ls -1 $(DIST)

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
