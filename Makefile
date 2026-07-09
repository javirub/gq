GOPATH_BIN := $(shell go env GOPATH)/bin

.PHONY: build test race lint acceptance update-golden coverage fuzz bench snapshot check

build:
	go build -o gq$(shell go env GOEXE) ./cmd/gq

test:
	go test ./...

race:
	go test -race ./...

lint:
	golangci-lint run

acceptance:
	go test -v ./acceptance

update-golden:
	go test ./acceptance -update

coverage:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

fuzz:
	go test ./internal/codec -run '^$$' -fuzz '^FuzzParse$$' -fuzztime 60s
	go test ./internal/codec -run '^$$' -fuzz '^FuzzParseJson$$' -fuzztime 30s
	go test ./internal/codec -run '^$$' -fuzz '^FuzzScanLine$$' -fuzztime 30s
	go test ./internal/codec -run '^$$' -fuzz '^FuzzEncodeExpression$$' -fuzztime 30s

bench:
	go test -bench . -benchmem -run '^$$' ./internal/...

# signing and SBOMs need cosign/syft and only make sense in CI releases
snapshot:
	goreleaser release --snapshot --clean --skip=sign,sbom

check: lint test race acceptance
