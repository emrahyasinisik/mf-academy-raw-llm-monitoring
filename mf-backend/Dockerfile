# Multi-stage build (Go Day 76-80): compile in a full toolchain image, then
# copy just the static binary into a tiny, non-root runtime image.

# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO disabled -> a fully static binary that runs on a minimal base image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app ./cmd/server

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser
COPY --from=build /app /app
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app"]
