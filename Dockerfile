# Build stage - compile from local source. Uses the same Alpine builder as
# docker/tradebot/Dockerfile for the tradebot binary; only the runtime stage
# differs, since this image additionally needs a real (virtual) display.
FROM golang:1.24.11-alpine AS builder

WORKDIR /src

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the tradebot binary for Linux, plus the playwright-go CLI (used
# below, at build time, to install Chromium -- keeping this a plain `go
# build` from our own go.mod means the driver/browser versions always match
# what etrade/autologin actually links against).
RUN CGO_ENABLED=0 go build -o /out/tradebot .
RUN CGO_ENABLED=0 go build -o /out/playwright github.com/mxschmitt/playwright-go/cmd/playwright

# Runtime stage. debian:trixie-slim (not Alpine)
FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        tzdata \
        tigervnc-standalone-server \
        tigervnc-tools \
        chromium \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/playwright /usr/local/bin/playwright
RUN chmod +x /usr/local/bin/playwright

# Bake Chromium + its runtime shared-library dependencies into the image at
# build time (--with-deps apt-get installs those deps), so containers don't
# re-download the browser on every start.
RUN playwright install --with-deps chromium && rm /usr/local/bin/playwright

COPY --from=builder /out/tradebot /usr/local/bin/tradebot
RUN chmod +x /usr/local/bin/tradebot

ENTRYPOINT ["/usr/local/bin/tradebot", "run"]
