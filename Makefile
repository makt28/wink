BINARY_NAME=wink
GO=go
LDFLAGS=-s -w

.PHONY: build dev clean fmt vet test docker cross tailwind

tailwind:
	npx tailwindcss -i web/static/input.css -o web/static/tailwind.css --minify

build: tailwind fmt vet
	$(GO) build -o $(BINARY_NAME) ./cmd/server

dev:
	$(GO) run ./cmd/server

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

clean:
	rm -f $(BINARY_NAME)
	rm -rf dist

cross: tailwind fmt vet
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/wink-linux-amd64       ./cmd/server
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/wink-linux-arm64       ./cmd/server
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/wink-darwin-arm64      ./cmd/server
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/wink-windows-amd64.exe ./cmd/server

docker:
	docker build -t wink -f deploy/Dockerfile .
