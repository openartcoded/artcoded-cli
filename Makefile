VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# linux, x86_64 (amd64) and arm64
PLATFORMS := linux/amd64 linux/arm64

IMAGE ?= nbittich/artcoded-cli

.PHONY: build install test dist checksums docker clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o artcoded .

install:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $${GOBIN:-$$(go env GOPATH)/bin}/artcoded .

test:
	go vet ./... && go test ./...

# dist/artcoded-<os>-<arch>[.exe] + dist/checksums.txt (the names install.sh downloads)
dist:
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "dist/artcoded-$$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/artcoded-$$os-$$arch . || exit 1; \
	done
	@$(MAKE) --no-print-directory checksums

checksums:
	@cd dist && sha256sum artcoded-* > checksums.txt && cat checksums.txt

# the image of the current platform, locally (the workflows build amd64 + arm64)
docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):dev .

clean:
	rm -rf artcoded dist
