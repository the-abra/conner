# Stage 1: Build
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache build-base
WORKDIR /app
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o bin/conner ./cmd/conner/main.go

# Stage 2: Runtime
FROM alpine:latest
# Install only optional tools (shred, img2sixel)
RUN apk add --no-cache coreutils libsixel-bin
WORKDIR /app
COPY --from=builder /app/bin/conner .

# Ensure the app can run without root by default
RUN adduser -D conner
USER conner

EXPOSE 80 6666
CMD ["./conner", "--server"]
