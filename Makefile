all=build
BIN_NAME=go-pluginserver

build:
	go build -ldflags="-s -w" -o $(BIN_NAME) -v

clean:
	rm -rf $(BIN_NAME)

.PHONY: all build clean
