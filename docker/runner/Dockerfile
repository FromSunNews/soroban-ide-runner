# ─── Soroban Runner Container ─────────────────────────────────────
# This container stays alive and receives `docker exec` commands
# from the backend to compile Soroban contracts.
#
# NOTE: First build will take 15-20 minutes as it compiles stellar-cli
# from source. Subsequent builds will be cached by Docker.

FROM rust:1.94-slim-bookworm

# Install system dependencies required for building stellar-cli
RUN apt-get update && apt-get install -y \
    build-essential \
    pkg-config \
    libssl-dev \
    libudev-dev \
    libusb-1.0-0-dev \
    libhidapi-dev \
    libdbus-1-dev \
    && rm -rf /var/lib/apt/lists/*

# Add the WebAssembly compilation target (required for Soroban contracts)
RUN rustup target add wasm32v1-none

# Install stellar-cli (this takes a while - be patient!)
RUN cargo install --locked stellar-cli --version 25.2.0

# Create the shared workspace directory
RUN mkdir -p /app/workspaces

WORKDIR /app/workspaces

# Keep the container alive to receive `docker exec` commands
CMD ["tail", "-f", "/dev/null"]
