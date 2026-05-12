FROM golang:alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy
RUN go build -o bin/conner ./cmd/conner/main.go

FROM alpine:latest
RUN apk add --no-cache libc6-compat tor nginx iptables coreutils
WORKDIR /app
COPY --from=builder /app/bin/conner .
EXPOSE 80 6666
CMD ["./conner", "--server", "--stealth"]
