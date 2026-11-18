FROM golang:1.27.4-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/zhonghui-api ./cmd/server

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
    && addgroup -S zhonghui \
    && adduser -S -G zhonghui zhonghui
WORKDIR /app
COPY --from=build /out/zhonghui-api /app/zhonghui-api
ENV PORT=8000
EXPOSE 8000
USER zhonghui
CMD ["/app/zhonghui-api"]
