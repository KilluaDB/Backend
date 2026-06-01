# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -o /pgproxy ./cmd/pgproxy

# Run stage
FROM alpine:3.21
RUN apk --no-cache add ca-certificates postgresql16-client mongodb-tools
WORKDIR /app
COPY --from=builder /api .
COPY --from=builder /pgproxy .
EXPOSE 8080
ENTRYPOINT ["/app/api"]
