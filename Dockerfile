# 多階段建置:最終 image 只有 binary,沒有 Go toolchain。
ARG SERVICE=device-service

FROM golang:1.26-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/${SERVICE}
RUN CGO_ENABLED=0 go build -trimpath -o /out/healthcheck ./cmd/healthcheck

FROM alpine:3.21
RUN adduser -D -u 10001 app
COPY --from=build /out/app /app/app
COPY --from=build /out/healthcheck /app/healthcheck
USER app
ENTRYPOINT ["/app/app"]
