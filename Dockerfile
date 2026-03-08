# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS go-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/4subs ./cmd/server

FROM alpine:3.21
ARG VERSION=dev
ARG REVISION=local
ARG CREATED=unknown
RUN apk add --no-cache ca-certificates ffmpeg tzdata \
    && adduser -D -u 10001 appuser \
    && mkdir -p /app/data /app/work /app/config /app/subtitles /app/web/dist /media \
    && chown -R appuser:appuser /app /media

LABEL org.opencontainers.image.title="4subs" \
      org.opencontainers.image.description="Bilingual subtitle generation platform built with Go and PrimeVue" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.created=$CREATED \
      org.opencontainers.image.source="https://github.com/gayhub/4subs"

COPY --from=go-build /out/4subs /app/4subs
COPY --from=web-build /src/web/dist /app/web/dist

USER appuser
WORKDIR /app
EXPOSE 8080

ENV HTTP_ADDR=:8080 \
    DATA_DIR=/app/data \
    WORK_DIR=/app/work \
    CONFIG_DIR=/app/config \
    STATIC_DIR=/app/web/dist \
    SUBTITLE_OUTPUT_PATH=/app/subtitles \
    MEDIA_PATHS=/media \
    FFMPEG_BIN=ffmpeg \
    TRANSLATION_PROVIDER=deepseek \
    DEEPSEEK_BASE_URL=https://api.deepseek.com \
    DEEPSEEK_MODEL=deepseek-chat

ENTRYPOINT ["/app/4subs"]
