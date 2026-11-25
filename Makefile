BINARY_NAME := door
BUILD_DIR := bin
LDFLAGS := -s -w

.PHONY: all test vet fmt build build-linux dev web-build clean

all: vet test build

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test -race -v ./...

web-install:
	pnpm --dir web install --frozen-lockfile

web-build:
	pnpm --dir web build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/door

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/door
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/door

dev:
	go run ./cmd/door serve --dev --addr :12345

clean:
	rm -rf $(BUILD_DIR)
