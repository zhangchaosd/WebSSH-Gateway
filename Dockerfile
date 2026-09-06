FROM node:24-alpine AS ui
WORKDIR /src
COPY package.json package-lock.json vite.config.js index.html ./
COPY web ./web
RUN npm ci --ignore-scripts --no-audit --no-fund && npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/httpapi/dist ./internal/httpapi/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/webssh ./cmd/webssh

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/webssh /webssh
VOLUME ["/data"]
ENV WEBSSH_LISTEN=0.0.0.0:8080 WEBSSH_DATA_DIR=/data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD ["/webssh", "healthcheck"]
ENTRYPOINT ["/webssh"]
