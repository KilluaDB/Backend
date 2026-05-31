# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api ./cmd/api

# Run stage
FROM alpine:3.21
RUN apk --no-cache add ca-certificates postgresql17-client mongodb-tools
WORKDIR /app
COPY --from=builder /api .
EXPOSE 8080
ENTRYPOINT ["/app/api"]
