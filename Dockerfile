# syntax=docker/dockerfile:1
# artcoded: the whatsapp bot (ENABLE_WHATSAPP_BOT=true; else it just waits) and the cli (docker exec ... artcoded <command>).
# Go cross-compiles: the build stage runs on the platform of the builder (no emulation), the final
# stage has no RUN, so linux/amd64 and linux/arm64 images build fast.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/artcoded .

FROM alpine:3.22
COPY --from=build /out/artcoded /usr/local/bin/artcoded
# config, tokens and the whatsapp session live in the /data volume;
# the image is updated by pulling a new one, not by the cli
ENV HOME=/data \
    ARTCODED_CONFIG=/data/artcoded-cli.yml \
    XDG_CACHE_HOME=/data/cache \
    WHATSAPP_SESSION=/data/whatsapp.db \
    DISABLE_AUTO_UPDATE=true
VOLUME /data
WORKDIR /data
ENTRYPOINT ["artcoded"]
CMD ["bot"]
