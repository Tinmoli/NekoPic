FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY assets.go config.example.toml ./
COPY app ./app
COPY internal ./internal
COPY web ./web
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/nekopic ./app

FROM alpine:3.22
RUN addgroup -S -g 10001 nekopic && adduser -S -u 10001 -G nekopic nekopic \
    && mkdir -p /app/view/pc /app/view/pe /app/logs && chown -R nekopic:nekopic /app
WORKDIR /app
COPY --from=build /out/nekopic /app/nekopic
COPY config.docker.toml /app/config.toml
USER 10001:10001
EXPOSE 8505
ENTRYPOINT ["/app/nekopic"]
CMD ["-c", "/app/config.toml"]
