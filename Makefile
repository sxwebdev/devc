BINARY := devc
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
SECRETFS_DIR := internal/secretfsbin/embedded

.PHONY: build test clean install lint release secretfs-bin

# Cross-build the devc-secretfs FUSE helper for the container architectures and
# place it where go:embed picks it up. Pure Go (no cgo), so it cross-compiles
# from any host. devc copies the matching binary into the container at runtime,
# so the image needs no extra packages.
secretfs-bin:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o $(SECRETFS_DIR)/devc-secretfs-linux-amd64 ./cmd/secretfs
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w" -o $(SECRETFS_DIR)/devc-secretfs-linux-arm64 ./cmd/secretfs

build: secretfs-bin
	go build $(LDFLAGS) -o bin/$(BINARY) .

install: secretfs-bin
	go install $(LDFLAGS) .

test: secretfs-bin
	go test ./...

lint:
	go vet ./...

release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v1.2.3"; exit 1; fi
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)

clean:
	rm -rf bin/

fmt:
	gofumpt -l -w .
