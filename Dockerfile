# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.26.2
FROM golang:${GO_VERSION}-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.work go.work.sum* ./
COPY gen/ gen/
COPY pkg/shared/ pkg/shared/
COPY services/ services/
ARG SERVICE
RUN test -n "$SERVICE"
WORKDIR /src/services/${SERVICE}
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/service /service
USER nonroot:nonroot
ENTRYPOINT ["/service"]
