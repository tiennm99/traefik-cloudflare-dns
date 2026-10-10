FROM --platform=$BUILDPLATFORM golang:1-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/traefik-cloudflare-dns .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/traefik-cloudflare-dns /traefik-cloudflare-dns
USER nonroot:nonroot
ENTRYPOINT ["/traefik-cloudflare-dns"]
