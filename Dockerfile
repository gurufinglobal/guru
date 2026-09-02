# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

# Keep these tags and multi-architecture digests together when updating them.
ARG GO_IMAGE=golang:1.26.7-bookworm@sha256:e8c859f5632dcfde7b32d2012b4351728f6437930887c2f6a91ea242459e5514
ARG RUNTIME_IMAGE=debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

FROM ${GO_IMAGE} AS gurud-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    test "${TARGETOS}" = "linux" \
    && test -n "${TARGETARCH}" \
    && make build-gurud \
       BUILD_DIR=/out \
       VERSION="${VERSION}" \
       COMMIT="${COMMIT}"

FROM ${RUNTIME_IMAGE} AS gurud-runtime

ARG VERSION=dev
ARG COMMIT=unknown
ARG SOURCE_URL=https://github.com/gurufinglobal/guru

LABEL org.opencontainers.image.title="Guru node" \
      org.opencontainers.image.description="Guru Cosmos EVM node runtime" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive \
       apt-get install --yes --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1025 guru \
    && useradd --uid 1025 --gid 1025 --home-dir /var/lib/guru \
         --create-home --shell /usr/sbin/nologin guru \
    && install --directory --owner 1025 --group 1025 /var/lib/guru/.gurud

COPY --from=gurud-builder --chown=1025:1025 /out/gurud /usr/local/bin/gurud
COPY --chown=1025:1025 LICENSE /usr/share/licenses/guru/LICENSE

ENV HOME=/var/lib/guru
WORKDIR /var/lib/guru
USER 1025:1025

VOLUME ["/var/lib/guru/.gurud"]
EXPOSE 26656 26657 9090 1317 8545 8546

HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=12 \
  CMD ["gurud", "status", "--node", "tcp://127.0.0.1:26657"]

ENTRYPOINT ["/usr/local/bin/gurud"]
CMD ["start", "--home=/var/lib/guru/.gurud"]
