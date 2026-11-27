FROM golang:1.26.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/seoos ./cmd/seoos

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates poppler-utils \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/seoos /usr/local/bin/seoos
USER 65532:65532
EXPOSE 8789
ENTRYPOINT ["/usr/local/bin/seoos"]
CMD ["serve"]


FROM node:24-bookworm-slim AS playwright
RUN npm install --ignore-scripts --prefix /opt/seoos-browser playwright@1.65.1

FROM runtime AS browser
USER root
RUN apt-get update && apt-get install -y --no-install-recommends chromium \
    && rm -rf /var/lib/apt/lists/*
COPY --from=playwright /usr/local/bin/node /usr/local/bin/node
COPY --from=playwright /opt/seoos-browser /opt/seoos-browser
COPY scripts/browser_extract.cjs /usr/local/lib/seoos/browser_extract.cjs
ENV SEOOS_BROWSER_RENDERER=/usr/local/lib/seoos/browser_extract.cjs \
    SEOOS_PLAYWRIGHT_MODULE=/opt/seoos-browser/node_modules/playwright \
    SEOOS_CHROMIUM_EXECUTABLE=/usr/bin/chromium \
    HOME=/tmp
USER 65532:65532

FROM runtime AS default
