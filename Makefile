.PHONY: build install test test-unit test-integration clean docker-build fmt

# Build the plgo CLI tool
build:
	cd cmd/plgo && go build -o ../../build/plgo .

# Install the plgo CLI into $GOPATH/bin
install:
	cd cmd/plgo && go install .

# Run all tests (unit + integration)
test: test-unit test-integration

# Run code-generator unit tests (no database required)
test-unit:
	cd cmd/plgo && go test -v ./...

# Detect Docker command — use flatpak-spawn if inside a Flatpak sandbox
DOCKER := $(shell command -v docker 2>/dev/null || echo "flatpak-spawn --host docker")

# Run integration tests using testcontainers (requires Docker)
test-integration:
	go test -tags integration -v -timeout 5m ./integration/

# Build the Docker image (verifies CLI + extension builds)
docker-build:
	$(DOCKER) build -t plgo-test .

# Format Go files
fmt:
	go run mvdan.cc/gofumpt@latest -w .
	@-find . -regex '.*\.\(js\|jsx\|ts\|tsx\|json\|yaml\|yml\|md\|markdown\|html\|css\|scss\|less\|vue\|svelte\|graphql\|gql\|mdx\)$$' -print0 | xargs -0 npx prettier --write 2>/dev/null || true


# Remove build artifacts
clean:
	rm -rf build/ example/build/ test/build/
	-$(DOCKER) rmi plgo-test 2>/dev/null || true
