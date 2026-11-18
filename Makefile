.PHONY: build run tidy fmt vet clean

BINARY_NAME ?= kuberush
BUILD_DIR ?= bin

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/kuberush

run:
	go run ./cmd/kuberush --config config.example.yaml

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR)
