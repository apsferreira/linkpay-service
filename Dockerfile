FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux go build -o linkpay-service ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/linkpay-service .
EXPOSE 3020
CMD ["./linkpay-service"]
