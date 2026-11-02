# anam-mcp as a service: the adapter over HTTP behind Cloudflare Access, on a
# distroless base, as a non-root user.
#
# .dockerignore admits only what the binary is built from. The binary is static;
# timetzdata embeds the zone database so ANAM_ZONE resolves without the base's,
# and the base's certificates let the adapter fetch the Access signing keys.
#
# Version identity comes in as build args from scripts/version.sh, as the Makefile
# passes it in ldflags; "dev" is the honest fallback.
#
# The image sets no configuration: ANAM_SERVER, ANAM_ZONE, ANAM_TOKEN_FILE (a
# mounted file the nonroot user can read), ANAM_MCP_HTTP_ADDR and the two Access
# variables come from the deployment. See the README's configuration table.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
    -ldflags "-s -w -X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/anam-mcp ./cmd/anam-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/anam-mcp /usr/local/bin/anam-mcp
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/anam-mcp"]
