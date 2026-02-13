# Builder stage
FROM golang:1.26-alpine3.23 AS builder

WORKDIR /app

# Install git for fetch dependencies
# RUN apk add --no-cache git 

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the binary
# -ldflags="-w -s" reduces binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o noemx21-bot ./cmd/noemx21-bot

# Final stage
FROM alpine:3.23

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create a non-root user
RUN adduser -D -g '' appuser

COPY --from=builder /app/noemx21-bot .
# COPY --from=builder /app/migrations ./migrations 
# Uncomment existing migration copy if you have migration files

USER appuser

CMD ["./noemx21-bot"]
