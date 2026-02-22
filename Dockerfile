FROM golang:1.21-alpine AS builder
WORKDIR /app

COPY . .

ARG VERSION=1.0.0
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o sfu ./cmd/sfu

FROM alpine:3
COPY --from=builder /app/sfu /usr/local/bin/sfu

RUN addgroup -g 1001 -S gryt && adduser -S gryt -u 1001 -G gryt
USER gryt

EXPOSE 5005

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:5005/health || exit 1

ENTRYPOINT ["sfu"]
