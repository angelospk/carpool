GOBIN := $(shell go env GOPATH)/bin
STATICCHECK := $(GOBIN)/staticcheck

.PHONY: check vet staticcheck test build tools clean

## check: the full gate — everything CI runs
check: vet staticcheck test

vet:
	go vet ./...

staticcheck: $(STATICCHECK)
	$(STATICCHECK) ./...

$(STATICCHECK):
	go install honnef.co/go/tools/cmd/staticcheck@latest

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
