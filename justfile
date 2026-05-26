_default:
    @just --list --unsorted

# Install dev tooling (golangci-lint)
init:
    @make init

# Run golangci-lint over the repo
lint:
    @make lint

# go generate
generate:
    @make generate

# go fmt
format:
    @make format

# Remove target/ and run go mod tidy
clean:
    @make clean

# Run tests with race detector and coverage
test:
    @make test

# go mod verify + go vet
verify:
    @make verify

# go build of all packages (no artifacts)
compile:
    @make compile

# Full cross-platform build (darwin/linux/windows x amd64/arm64)
build:
    @make build

# Fast build only for the host OS/arch
build-local:
    @make build-local

# Run integration tests
integration-tests:
    @make run-integration-tests
