# ─── Soroban Runner Container ─────────────────────────────────────
# This container stays alive and receives `docker exec` commands
# from the backend to compile Soroban contracts.
#
# NOTE: First build will take 15-20 minutes as it compiles stellar-cli
# from source. Subsequent builds will be cached by Docker.

FROM rust:1.94-slim-bookworm

# Install dependency dasar (tetap dibutuhkan untuk workflow kamu)
RUN apt-get update && apt-get install -y \
    curl \
    ca-certificates \
    build-essential \
    pkg-config \
    libssl-dev \
    libudev-dev \
    libusb-1.0-0-dev \
    libhidapi-dev \
    libdbus-1-dev \
    && rm -rf /var/lib/apt/lists/*

# Add WASM target (ini tetap penting buat Soroban contract)
RUN rustup target add wasm32v1-none

# ✅ Install stellar-cli pre-built binary
RUN ARCH=$(dpkg --print-architecture) && \
    if [ "$ARCH" = "amd64" ]; then \
      STELLAR_ARCH="x86_64-unknown-linux-gnu"; \
    elif [ "$ARCH" = "arm64" ]; then \
      STELLAR_ARCH="aarch64-unknown-linux-gnu"; \
    else \
      echo "Unsupported architecture: $ARCH" && exit 1; \
    fi && \
    echo "Downloading stellar-cli for $STELLAR_ARCH..." && \
    curl -fSL -o /tmp/stellar-cli.tar.gz \
      "https://github.com/stellar/stellar-cli/releases/download/v25.2.0/stellar-cli-25.2.0-${STELLAR_ARCH}.tar.gz" && \
    tar -xzf /tmp/stellar-cli.tar.gz -C /tmp && \
    find /tmp -name "stellar" -type f -exec mv {} /usr/local/bin/stellar \; && \
    chmod +x /usr/local/bin/stellar && \
    rm -rf /tmp/stellar-cli.tar.gz

# Optional: verifikasi
RUN stellar --version

# Workspace
RUN mkdir -p /app/workspaces
WORKDIR /app/workspaces

# Keep the container alive to receive `docker exec` commands
CMD ["tail", "-f", "/dev/null"]
