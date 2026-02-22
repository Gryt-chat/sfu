# Multi-stage build for Gryt SFU Server
FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=1.0.0

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X main.Version=${VERSION}" -a -installsuffix cgo -o sfu ./cmd/sfu

# Production stage -- scratch-like Alpine with no package installs needed
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/sfu .

RUN addgroup -g 1001 -S gryt \
  && adduser -S gryt -u 1001 -G gryt \
  && chown gryt:gryt /root/sfu
USER gryt

EXPOSE 5005

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:5005/health || exit 1

CMD ["./sfu"]
