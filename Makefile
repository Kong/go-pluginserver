BIN_NAME=go-pluginserver
VERSION?=development

all: build

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BIN_NAME) -v

clean:
	rm -rf $(BIN_NAME)

.PHONY: all build clean
