[![Build Status][badge-travis-image]][badge-travis-url]
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/Kong/kong/blob/master/LICENSE)

# Kong Go Pluginserver

Kong sidecar process to load and execute Go plugins.

## Usage

Building:
```
make
```

This will result in a `go-pluginserver` executable; place it in an appropriate
location in your file system.

On the Kong configuration file, set the `go_pluginserver_exe` property:
```
go_pluginserver_exe = /my/path/to/pluginserver
```

Or, alternatively, set the `KONG_GO_PLUGINSERVER_EXE` environment variable.

## Implementation

See [docs/overview.md](docs/overview.md) for details on how the pluginserver
works.

---

[badge-travis-url]: https://travis-ci.com/Kong/go-pluginserver/branches
[badge-travis-image]: https://api.travis-ci.com/Kong/go-pluginserver.svg?branch=master

