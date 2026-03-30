FROM golang:1.26-bookworm AS builder

# Add PostgreSQL apt repo for PG 18
RUN apt-get update && apt-get install -y curl ca-certificates gnupg lsb-release && \
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /usr/share/keyrings/pgdg.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/pgdg.gpg] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list && \
    apt-get update && \
    apt-get install -y postgresql-server-dev-18 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Run code-generator unit tests (no PG needed)
RUN cd cmd/plgo && go test -v ./...

# Build the plgo CLI
RUN cd cmd/plgo && go build -o /usr/local/bin/plgo .

# Build the example extension
RUN cd example && /usr/local/bin/plgo .

# Build the test extension
RUN cd test && /usr/local/bin/plgo .

# --- Runtime stage: install into PG 18 and run integration tests ---
FROM postgres:18

RUN apt-get update && apt-get install -y make postgresql-server-dev-18 && rm -rf /var/lib/apt/lists/*

# Install example extension
COPY --from=builder /src/example/build/ /tmp/example-ext/
RUN cd /tmp/example-ext && make install with_llvm=no && rm -rf /tmp/example-ext

# Install test extension
COPY --from=builder /src/test/build/ /tmp/test-ext/
RUN cd /tmp/test-ext && make install with_llvm=no && rm -rf /tmp/test-ext

# Copy SQL integration tests
COPY test/sql/ /test/sql/
COPY test/expected/ /test/expected/

COPY test/run-integration.sh /test/run-integration.sh
RUN chmod +x /test/run-integration.sh

CMD ["/test/run-integration.sh"]
