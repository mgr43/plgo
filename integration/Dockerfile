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

# Build the plgo CLI
RUN cd cmd/plgo && go build -o /usr/local/bin/plgo .

# Build the example extension
RUN cd example && /usr/local/bin/plgo .

# Build the test extension
RUN cd test && /usr/local/bin/plgo .

# --- Runtime stage: PG 18 with extensions installed ---
FROM postgres:18

RUN apt-get update && apt-get install -y make postgresql-server-dev-18 && rm -rf /var/lib/apt/lists/*

# Install example extension
COPY --from=builder /src/example/build/ /tmp/example-ext/
RUN cd /tmp/example-ext && make install && rm -rf /tmp/example-ext

# Install test extension
COPY --from=builder /src/test/build/ /tmp/test-ext/
RUN cd /tmp/test-ext && make install && rm -rf /tmp/test-ext

# Default PG env for testcontainers
ENV POSTGRES_USER=postgres
ENV POSTGRES_PASSWORD=postgres
ENV POSTGRES_DB=plgo_test

