FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder
ARG TARGETARCH
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=1.0.0
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH go build -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o sfu ./cmd/sfu

FROM alpine:3
RUN apk add --no-cache wget
COPY --from=builder /app/sfu /usr/local/bin/sfu

RUN addgroup -g 1001 -S gryt && adduser -S gryt -u 1001 -G gryt
USER gryt

EXPOSE 5005

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:5005/health || exit 1

ENTRYPOINT ["sfu"]
