FROM golang:1.26.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/seoos ./cmd/seoos

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/seoos /usr/local/bin/seoos
USER 65532:65532
EXPOSE 8789
ENTRYPOINT ["/usr/local/bin/seoos"]
CMD ["serve"]
