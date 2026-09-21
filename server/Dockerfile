# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/registry .

# ---- run ----
# distroless: no shell, no package manager, runs as non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/registry /registry
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/registry", "healthcheck"]
ENTRYPOINT ["/registry"]
