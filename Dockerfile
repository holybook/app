# syntax=docker/dockerfile:1

# One image for everything: `serve` (default) runs the web server, `import`
# loads content into the database (see deploy/compose.yaml).

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /holybook ./cmd/holybook

# Static binary on a minimal base with CA certificates (for the Gemini API),
# running as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /holybook /holybook
EXPOSE 8080
ENTRYPOINT ["/holybook"]
CMD ["serve"]
