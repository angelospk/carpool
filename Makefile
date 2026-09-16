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

.PHONY: check vet staticcheck test test-race test-routed build clean

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

## test-routed: the tests that need a real OSRM as well as a database.
##
## They skip without CARPOOL_TEST_OSRM, which means `make check` does not run
## them and did not say so. What they cover is the one thing a handler test
## cannot: whether the deployed routing extract actually holds the roads the
## served area claims — a clipped extract answers Ok for a coordinate it does
## not have, so this is not a question that fails loudly on its own.
##
## Point it at the extract you are about to ship:
##   CARPOOL_TEST_OSRM=http://127.0.0.1:5000 make test-routed
test-routed:
	@test -n "$$CARPOOL_TEST_OSRM" || \
		{ echo "set CARPOOL_TEST_OSRM to a running osrm-routed, e.g. http://127.0.0.1:5000"; exit 1; }
	@test -n "$$CARPOOL_TEST_DSN" || \
		{ echo "set CARPOOL_TEST_DSN as well: these tests need a database too"; exit 1; }
	go test ./internal/api/ -run "AcrossAPrefectureBoundary|OutsideTheRoutedArea" -count=1 -v

## test-race: the same suite under the race detector
test-race:
	go test -race ./...

build:
	go build -o bin/api ./cmd/api

clean:
	rm -rf bin
