# Build: Debian bookworm (glibc) so go-libtor/CGO links.
# Runtime: distroless-ish slim Debian — not Alpine musl.
# No go mod tidy (locked go.sum).

FROM golang:1.26-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/conner ./cmd/conner

FROM debian:bookworm-slim
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/* \
	&& useradd --create-home --uid 1000 conner
WORKDIR /app
COPY --from=builder /app/bin/conner .
USER conner
EXPOSE 6666
ENTRYPOINT ["./conner"]
CMD ["--server", "--tor"]
