FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/emcache ./cmd/emcache

FROM alpine:3.22

WORKDIR /app

COPY --from=build /out/emcache /usr/local/bin/emcache

ENV HOST=0.0.0.0 \
    PORT=3000 \
    STORAGE_PATH=/app/storage

EXPOSE 3000

ENTRYPOINT ["emcache"]
