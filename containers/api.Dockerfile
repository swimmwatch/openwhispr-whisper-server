# Approved defaults match containers/build-inputs.json. Version changes also need
# matching source checksums and a separately qualified build-input inventory.
ARG GO_VERSION=1.27.3
ARG GO_BUILD_IMAGE=golang:${GO_VERSION}-bookworm@sha256:fea12ba16096adec0f699ce1c4da2154d320450e56c1f63671f95d2a3551c77b
ARG UBUNTU_IMAGE=ubuntu:24.04@sha256:a2d43bffc46670d7108bb3c0468c67c5557c4c92e83e1e34cbff1322ad1b94dc

ARG CURL_VERSION=8.5.0-2ubuntu10.16
ARG CA_CERTIFICATES_VERSION=20260816ubuntu0.24.04.1
ARG CCACHE_VERSION=4.9.1-1
ARG APT_RETRIES=3

ARG FFMPEG_VERSION=9.0.2
ARG FFMPEG_TAG=n${FFMPEG_VERSION}
ARG FFMPEG_COMMIT=946fcce07b6dcd0331c8cc609192aeff5e1924f8
ARG FFMPEG_SHA256=0aa2b1de2a5698b20a23e93d539a9a8e82ca0117496c5bdf05d198805f42bb3b
ARG FETCH_CONNECT_TIMEOUT=20
ARG FETCH_MAX_TIME=180
ARG FETCH_RETRIES=3

ARG RUNTIME_UID=10001
ARG RUNTIME_GID=10001
ARG IMAGE_SOURCE=https://github.com/swimmwatch/openwhispr-whisper-server

# Go inputs precede application source so module downloads remain reusable.
FROM ${GO_BUILD_IMAGE} AS go-build
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ARG GO_VERSION
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=whisper-go-mod-${GO_VERSION}-amd64,target=/go/pkg/mod <<BUILD
test "$(go env GOVERSION)" = "go${GO_VERSION}"
go mod download
go mod verify
BUILD

COPY cmd/api ./cmd/api
COPY internal ./internal
COPY LICENSE THIRD_PARTY_NOTICES.md ./
RUN --mount=type=cache,id=whisper-go-mod-${GO_VERSION}-amd64,target=/go/pkg/mod \
    --mount=type=cache,id=whisper-go-build-${GO_VERSION}-amd64,target=/root/.cache/go-build <<BUILD
go mod verify
mkdir -p /out/licenses/go
go build -trimpath -buildvcs=false -o /out/whisper-api ./cmd/api

# Retain executable provenance and redistributed module licenses.
go version -m /out/whisper-api > /out/go-build.txt
go list -m -f '{{if .Dir}}{{.Path}} {{.Version}} {{.Dir}}{{end}}' all > /out/go-modules.txt
while read -r module version directory; do
    if [ -z "$directory" ]; then
        continue
    fi
    target="/out/licenses/go/$module@$version"
    mkdir -p "$target"
    find "$directory" -maxdepth 1 -type f \
        \( -iname 'LICENSE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -name PATENTS \) \
        -exec cp '{}' "$target/" \;
done < /out/go-modules.txt
cp LICENSE THIRD_PARTY_NOTICES.md /out/licenses/
cp /usr/local/go/LICENSE /out/licenses/go/Go-LICENSE

find cmd internal -type f -print0 | sort -z | xargs -0 sha256sum > /out/go-sources.txt
sha256sum go.mod go.sum >> /out/go-sources.txt
sha256sum /out/go-sources.txt > /out/go-sources.sha256
BUILD

# Decoder build tools stay outside the final image.
FROM ${UBUNTU_IMAGE} AS decoder-build
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ARG CURL_VERSION
ARG CA_CERTIFICATES_VERSION
ARG CCACHE_VERSION
ARG APT_RETRIES
ARG BUILD_ESSENTIAL_VERSION=12.10ubuntu1
ARG PKG_CONFIG_VERSION=1.8.1-2build1
ARG NASM_VERSION=2.16.01-1build1
RUN <<BUILD
apt-get -o APT::Update::Error-Mode=any -o Acquire::Retries="${APT_RETRIES}" update
apt-get install -y --no-install-recommends \
    "build-essential=${BUILD_ESSENTIAL_VERSION}" \
    "ca-certificates=${CA_CERTIFICATES_VERSION}" \
    "ccache=${CCACHE_VERSION}" \
    "curl=${CURL_VERSION}" \
    "nasm=${NASM_VERSION}" \
    "pkg-config=${PKG_CONFIG_VERSION}"
rm -rf /var/lib/apt/lists/*
BUILD

WORKDIR /src/ffmpeg
ARG FFMPEG_TAG
ARG FFMPEG_VERSION
ARG FFMPEG_COMMIT
ARG FFMPEG_SHA256
ARG FETCH_CONNECT_TIMEOUT
ARG FETCH_MAX_TIME
ARG FETCH_RETRIES
RUN <<BUILD
curl -fsSL \
    --connect-timeout "${FETCH_CONNECT_TIMEOUT}" \
    --max-time "${FETCH_MAX_TIME}" \
    --retry "${FETCH_RETRIES}" \
    "https://github.com/FFmpeg/FFmpeg/archive/${FFMPEG_COMMIT}.tar.gz" \
    --output /tmp/ffmpeg.tar.gz
printf '%s  %s\n' "${FFMPEG_SHA256}" /tmp/ffmpeg.tar.gz | sha256sum -c -
mkdir -p /out/sources
cp /tmp/ffmpeg.tar.gz "/out/sources/ffmpeg-${FFMPEG_TAG}.tar.gz"
tar -xzf /tmp/ffmpeg.tar.gz --strip-components=1
rm /tmp/ffmpeg.tar.gz
BUILD

ARG BUILD_JOBS
ARG BUILD_MEMORY_RESERVE_KIB=2097152
ARG BUILD_MEMORY_PER_JOB_KIB=524288
ENV CCACHE_DIR=/root/.cache/ccache \
    CCACHE_COMPILERCHECK=content
COPY --link --chmod=0555 containers/scripts/build-jobs.sh /usr/local/bin/whisper-build-jobs
RUN --mount=type=cache,id=whisper-ffmpeg-${FFMPEG_VERSION}-amd64,target=/root/.cache/ccache <<BUILD
jobs=$(/usr/local/bin/whisper-build-jobs)
printf 'BUILD_JOBS=%s\n' "$jobs"

# This bounded decoder profile is part of the release and license contract.
./configure \
    --cc='ccache gcc' \
    --cxx='ccache g++' \
    --prefix=/opt/ffmpeg \
    --disable-static \
    --enable-shared \
    --disable-doc \
    --disable-debug \
    --disable-autodetect \
    --disable-network \
    --disable-gpl \
    --disable-nonfree \
    --disable-ffplay \
    --disable-ffprobe \
    --disable-protocols \
    --enable-protocol=pipe,cache \
    --disable-demuxers \
    --enable-demuxer=wav,mp3,mov,ogg,flac,matroska,mpegps \
    --disable-decoder=h264 \
    --disable-parser=h264,dvdsub \
    --disable-encoders \
    --enable-encoder=pcm_s16le \
    --disable-muxers \
    --enable-muxer=pcm_s16le
make -j "$jobs"
make install
ccache --show-stats

mkdir -p /out/licenses/ffmpeg
cp COPYING.* LICENSE.md /out/licenses/ffmpeg/
cp ffbuild/config.mak /out/ffmpeg-config.mak
LD_LIBRARY_PATH=/opt/ffmpeg/lib /opt/ffmpeg/bin/ffmpeg -version > /out/ffmpeg-version.txt
dpkg-query -W > /out/decoder-builder-packages.txt
BUILD

FROM ${UBUNTU_IMAGE} AS runtime
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ARG CURL_VERSION
ARG CA_CERTIFICATES_VERSION
ARG APT_RETRIES
ARG RUNTIME_UID
ARG RUNTIME_GID
RUN <<BUILD
for identity in "${RUNTIME_UID}" "${RUNTIME_GID}"; do
    case "$identity" in
        ''|0*|*[!0-9]*)
            echo 'Runtime UID/GID must be positive integers' >&2
            exit 2
            ;;
    esac
done

apt-get -o APT::Update::Error-Mode=any -o Acquire::Retries="${APT_RETRIES}" update
apt-get install -y --no-install-recommends \
    "ca-certificates=${CA_CERTIFICATES_VERSION}" \
    "curl=${CURL_VERSION}"
rm -rf /var/lib/apt/lists/*

groupadd --gid "${RUNTIME_GID}" whisper
useradd --uid "${RUNTIME_UID}" --gid "${RUNTIME_GID}" \
    --no-create-home --shell /usr/sbin/nologin whisper

mkdir -p /etc/whisper /tmp/whisper-api /usr/local/share/whisper
chown "${RUNTIME_UID}:${RUNTIME_GID}" /tmp/whisper-api
chmod 0700 /tmp/whisper-api
dpkg-query -W > /usr/local/share/whisper/runtime-packages.txt
BUILD

COPY --link --from=go-build /out/whisper-api /usr/local/bin/whisper-api
COPY --link --from=go-build /out/licenses /usr/local/share/whisper/licenses
COPY --link --from=go-build \
    /out/go-build.txt \
    /out/go-modules.txt \
    /out/go-sources.txt \
    /out/go-sources.sha256 \
    /usr/local/share/whisper/
COPY --link --from=decoder-build /opt/ffmpeg/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --link --from=decoder-build /opt/ffmpeg/lib/lib*.so* /usr/local/lib/
COPY --link --from=decoder-build /out/licenses/ /usr/local/share/whisper/licenses/
COPY --link --from=decoder-build /out/sources/ /usr/local/share/whisper/sources/
COPY --link --from=decoder-build \
    /out/ffmpeg-version.txt \
    /out/ffmpeg-config.mak \
    /out/decoder-builder-packages.txt \
    /usr/local/share/whisper/
COPY --link containers/build-inputs.json /usr/local/share/whisper/build-inputs.json

# Metadata is derived from effective arguments, rather than repeated literals.
ARG GO_VERSION
ARG UBUNTU_IMAGE
ARG FFMPEG_TAG
ARG FFMPEG_COMMIT
ARG FFMPEG_SHA256
RUN <<BUILD
ldconfig
cat > /usr/local/share/whisper/build.json <<JSON
{
  "go_version": "${GO_VERSION}",
  "gpu_build": false,
  "ffmpeg_tag": "${FFMPEG_TAG}",
  "ffmpeg_commit": "${FFMPEG_COMMIT}",
  "ffmpeg_sha256": "${FFMPEG_SHA256}",
  "runtime_base": "${UBUNTU_IMAGE}"
}
JSON
ldd /usr/local/bin/ffmpeg > /usr/local/share/whisper/linked-libraries.txt
! grep 'not found' /usr/local/share/whisper/linked-libraries.txt
BUILD

ARG SOURCE_REVISION=unreleased
ARG APP_VERSION=0.0.0-dev
ARG IMAGE_SOURCE
ARG IMAGE_TITLE=openwhispr-whisper-server-api
ARG IMAGE_DESCRIPTION="Resident Whisper public API and bounded FFmpeg decoder"
ARG IMAGE_AUTHORS="openwhispr-whisper-server contributors"
ARG IMAGE_VENDOR=${IMAGE_AUTHORS}
ARG IMAGE_URL=${IMAGE_SOURCE}
ARG IMAGE_DOCUMENTATION=${IMAGE_SOURCE}/blob/main/docs/container-builds.md
ARG IMAGE_LICENSES="Apache-2.0 AND MIT AND BSD-3-Clause AND LGPL-2.1-or-later"
LABEL org.opencontainers.image.source="${IMAGE_SOURCE}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.title="${IMAGE_TITLE}" \
      org.opencontainers.image.description="${IMAGE_DESCRIPTION}" \
      org.opencontainers.image.authors="${IMAGE_AUTHORS}" \
      org.opencontainers.image.vendor="${IMAGE_VENDOR}" \
      org.opencontainers.image.url="${IMAGE_URL}" \
      org.opencontainers.image.documentation="${IMAGE_DOCUMENTATION}" \
      org.opencontainers.image.licenses="${IMAGE_LICENSES}"

USER ${RUNTIME_UID}:${RUNTIME_GID}
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD ["curl", "--fail", "--silent", "--max-time", "2", "--output", "/dev/null", "http://127.0.0.1:8080/health/ready"]
ENTRYPOINT ["/usr/local/bin/whisper-api"]
