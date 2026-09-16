# The version is pinned here and nowhere else. `go run pkg@version` builds the
# tool with whatever Go toolchain is installed, every time, and that is the
# point: the previous target installed `@latest` into GOPATH/bin and then only
# ever reinstalled it if the file was *missing*. So a binary built by Go 1.25
# survived the upgrade to 1.26 and refused to parse the new standard library —
# "file requires newer Go version go1.26 (application built with go1.25)" — and
# `make check` had been failing on that alone for weeks. A stale binary is not a
# state this can be in any more.
#
# Deliberately not a `tool` directive in go.mod: honnef.co/go/tools requires
# go 1.26, so adding it there raises this module's own go directive to 1.26 and
# the api image builds `FROM golang:1.25-bookworm`. A linter must not decide
# which Go builds the product.
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.8.1

.PHONY: check vet staticcheck test build tools clean

## check: the full gate — everything CI runs
check: vet staticcheck test

vet:
	go vet ./...

staticcheck:
	$(STATICCHECK) ./...

## test: unit tests, plus integration tests when CARPOOL_TEST_DSN is set
## (integration tests skip themselves otherwise)
test:
	go test ./...

## test-race: the same suite under the race detector
test-race:
	go test -race ./...

build:
	go build -o bin/api ./cmd/api

clean:
	rm -rf bin
