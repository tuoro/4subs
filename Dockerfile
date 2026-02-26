# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.23-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/4subs ./cmd/server

FROM alpine:3.21
RUN adduser -D -u 10001 appuser \
    && mkdir -p /app/data /app/config /app/subtitles /app/web/dist /media \
    && chown -R appuser:appuser /app /media

COPY --from=go-build /out/4subs /app/4subs
COPY --from=web-build /src/web/dist /app/web/dist

USER appuser
WORKDIR /app
EXPOSE 8080

ENV HTTP_ADDR=:8080 \
    DATA_DIR=/app/data \
    CONFIG_DIR=/app/config \
    STATIC_DIR=/app/web/dist \
    SUBTITLE_OUTPUT_PATH=/app/subtitles \
    MEDIA_PATHS=/media

ENTRYPOINT ["/app/4subs"]
