# Builder stage
FROM golang:1.26.1-alpine3.23 AS builder

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

# Prepare runtime directories for non-root execution.
RUN mkdir -p /app/docs/specs /app/tmp /app/data_repo && chown -R appuser:appuser /app

COPY --from=builder --chown=appuser:appuser /app/noemx21-bot .
COPY --from=builder --chown=appuser:appuser /app/docs/specs/flows ./docs/specs/flows

USER appuser

CMD ["./noemx21-bot"]
