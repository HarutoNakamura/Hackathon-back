# ビルドステージ
FROM golang:1.23 AS builder
WORKDIR /app

# 必要なファイルをコピー
COPY go.mod go.sum ./
RUN go mod download

# ソースコードとTLS証明書をコピー
COPY . ./

# 静的バイナリをビルド（CGO無効化）
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# 実行ステージ
FROM scratch
WORKDIR /app

# 必要なファイルをコピー
COPY --from=builder /app/main .
COPY ./server-ca.pem ./client-cert.pem ./client-key.pem ./

# アプリケーションを実行
EXPOSE 8080
CMD ["./main"]
