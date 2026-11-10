# ---------- 构建阶段 ----------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# 先只拷依赖文件，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/gochat ./cmd/server

# ---------- 运行阶段 ----------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 gochat

WORKDIR /app

COPY --from=builder /out/gochat /app/gochat
COPY configs/config.yaml /app/configs/config.yaml
COPY migrations /app/migrations

USER gochat

EXPOSE 8080

ENTRYPOINT ["/app/gochat"]
