.PHONY: build test lint fmt vet clean

BINARY := gcode
BUILD_DIR := bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/gcode

test:
	go test ./...

test-integration:
	go test -tags integration ./...

lint: vet fmt-check

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

clean:
	rm -rf $(BUILD_DIR)

all: fmt vet test build
