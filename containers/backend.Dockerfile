# Approved defaults match containers/build-inputs.json. Source/version overrides
# require matching checksums and a separately qualified build-input inventory.
ARG GO_VERSION=1.27.3
ARG GO_BUILD_IMAGE=golang:${GO_VERSION}-bookworm@sha256:fea12ba16096adec0f699ce1c4da2154d320450e56c1f63671f95d2a3551c77b
ARG CUDA_TOOLKIT_VERSION=12.8.1
ARG CUDA_BUILD_IMAGE=nvidia/cuda:${CUDA_TOOLKIT_VERSION}-devel-ubuntu24.04@sha256:4b9ed5fa8361736996499f64ecebf25d4ec37ff56e4d11323ccde10aa36e0c43
ARG CUDA_RUNTIME_IMAGE=nvidia/cuda:${CUDA_TOOLKIT_VERSION}-runtime-ubuntu24.04@sha256:828c4d878adcaa4265d80c95d8ec877149b49bb2419a4cf3bb6aa889bbb7ca2e

ARG CURL_VERSION=8.5.0-2ubuntu10.16
ARG CA_CERTIFICATES_VERSION=20260816ubuntu0.24.04.1
ARG CCACHE_VERSION=4.9.1-1
ARG APT_RETRIES=3
ARG FETCH_CONNECT_TIMEOUT=20
ARG FETCH_MAX_TIME=180
ARG FETCH_RETRIES=3

ARG WHISPER_REVISION=6ac14546046ffb17d144a08bcad65337e535cf5
ARG WHISPER_COMMIT=6ac14546046ffb17d144a08bcad65337e535cf5f
ARG WHISPER_SHA256=ef9c2b2e765c245686acb86b3f4668eb70a83894d1d5f06164206217979073ad
ARG WHISPER_SERVER_BLOB=b87ef27375fd614d9e7c9698ecda94b03e548eb6
ARG WHISPER_SERVER_SHA256=4edae36f55e3a751583cd2dfe32bb5b5516cf59a3c137254f08c7ac0bd3f5d59
ARG CMAKE_VERSION=3.31.10
ARG CMAKE_SHA256=3cb3dd247b6a1de2d0f4b20c6fd4326c9024e894cebc9dc8699758887e566ca7
ARG CUDA_ARCHITECTURES=50-virtual;61-virtual;70-virtual;75-virtual;80-virtual;86-real;89-real;90-virtual;120a-real
ARG CUDA_EULA_SHA256=6722d4c310a2ec9ad869ede3371a648fe2cfc2baaf8e1ece2c35e1dccc05752c

ARG RUNTIME_UID=10001
ARG RUNTIME_GID=10001
ARG IMAGE_SOURCE=https://github.com/swimmwatch/openwhispr-whisper-server

# Module inputs remain separate from application source for cache reuse.
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

COPY cmd/backend ./cmd/backend
COPY internal ./internal
COPY LICENSE THIRD_PARTY_NOTICES.md ./
RUN --mount=type=cache,id=whisper-go-mod-${GO_VERSION}-amd64,target=/go/pkg/mod \
    --mount=type=cache,id=whisper-go-build-${GO_VERSION}-amd64,target=/root/.cache/go-build <<BUILD
test ! -e /dev/nvidia0
go mod verify
mkdir -p /out/licenses/go
go build -trimpath -buildvcs=false -o /out/whisper-backend ./cmd/backend

go version -m /out/whisper-backend > /out/go-build.txt
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

FROM ${CUDA_BUILD_IMAGE} AS native-build
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ARG CURL_VERSION
ARG CA_CERTIFICATES_VERSION
ARG CCACHE_VERSION
ARG APT_RETRIES
RUN <<BUILD
rm -f /etc/apt/sources.list.d/cuda*.list /etc/apt/sources.list.d/nvidia*.list
apt-get -o APT::Update::Error-Mode=any -o Acquire::Retries="${APT_RETRIES}" update
apt-get install -y --no-install-recommends \
    "ca-certificates=${CA_CERTIFICATES_VERSION}" \
    "ccache=${CCACHE_VERSION}" \
    "curl=${CURL_VERSION}"
rm -rf /var/lib/apt/lists/*
BUILD

WORKDIR /src/whisper
ARG WHISPER_COMMIT
ARG WHISPER_SHA256
ARG WHISPER_SERVER_SHA256
ARG CMAKE_VERSION
ARG CMAKE_SHA256
ARG FETCH_CONNECT_TIMEOUT
ARG FETCH_MAX_TIME
ARG FETCH_RETRIES
RUN <<BUILD
test ! -e /dev/nvidia0
curl -fsSL \
    --connect-timeout "${FETCH_CONNECT_TIMEOUT}" \
    --max-time "${FETCH_MAX_TIME}" \
    --retry "${FETCH_RETRIES}" \
    "https://github.com/Kitware/CMake/releases/download/v${CMAKE_VERSION}/cmake-${CMAKE_VERSION}-linux-x86_64.tar.gz" \
    --output /tmp/cmake.tar.gz
printf '%s  %s\n' "${CMAKE_SHA256}" /tmp/cmake.tar.gz | sha256sum -c -
tar -xzf /tmp/cmake.tar.gz -C /opt --strip-components=1
rm /tmp/cmake.tar.gz

curl -fsSL \
    --connect-timeout "${FETCH_CONNECT_TIMEOUT}" \
    --max-time "${FETCH_MAX_TIME}" \
    --retry "${FETCH_RETRIES}" \
    "https://github.com/OpenWhispr/whisper.cpp/archive/${WHISPER_COMMIT}.tar.gz" \
    --output /tmp/whisper.tar.gz
printf '%s  %s\n' "${WHISPER_SHA256}" /tmp/whisper.tar.gz | sha256sum -c -
tar -xzf /tmp/whisper.tar.gz --strip-components=1
rm /tmp/whisper.tar.gz
printf '%s  %s\n' "${WHISPER_SERVER_SHA256}" examples/server/server.cpp | sha256sum -c -
BUILD

ARG CUDA_TOOLKIT_VERSION
ARG CUDA_ARCHITECTURES
ARG BUILD_JOBS
ARG BUILD_MEMORY_RESERVE_KIB=2097152
ARG BUILD_MEMORY_PER_JOB_KIB=2097152
ENV CCACHE_DIR=/root/.cache/ccache \
    CCACHE_COMPILERCHECK=content
COPY --link --chmod=0555 containers/scripts/build-jobs.sh /usr/local/bin/whisper-build-jobs
RUN --mount=type=cache,id=whisper-cuda-${CUDA_TOOLKIT_VERSION}-amd64,target=/root/.cache/ccache <<BUILD
jobs=$(/usr/local/bin/whisper-build-jobs)
printf 'BUILD_JOBS=%s\n' "$jobs"

# CUDA and the unchanged native server remain mandatory for this backend.
/opt/bin/cmake -S . -B build \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_C_COMPILER_LAUNCHER=ccache \
    -DCMAKE_CXX_COMPILER_LAUNCHER=ccache \
    -DCMAKE_CUDA_COMPILER_LAUNCHER=ccache \
    -DGGML_NATIVE=OFF \
    -DGGML_CUDA=ON \
    -DWHISPER_CURL=OFF \
    "-DCMAKE_CUDA_ARCHITECTURES=${CUDA_ARCHITECTURES}" \
    -DWHISPER_BUILD_TESTS=OFF \
    -DWHISPER_BUILD_SERVER=ON
/opt/bin/cmake --build build --target whisper-server --parallel "$jobs"
ccache --show-stats

mkdir -p /out/bin /out/lib /out/licenses/whisper
cp build/bin/whisper-server /out/bin/
find build -name 'lib*.so*' -exec cp -L '{}' /out/lib/ \;
cp LICENSE /out/licenses/whisper/
cp --parents \
    examples/miniaudio.h \
    examples/stb_vorbis.c \
    examples/json.hpp \
    examples/server/httplib.h \
    /out/licenses/whisper/
find ggml examples/server -type f \
    \( -iname 'LICENSE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) \
    -exec cp --parents '{}' /out/licenses/whisper/ \;
grep -E '^(CMAKE_CUDA_ARCHITECTURES|GGML_NATIVE|GGML_CUDA|WHISPER_CURL|CMAKE_BUILD_TYPE):' \
    build/CMakeCache.txt > /out/native-build-flags.txt
nvcc --version > /out/cuda-compiler.txt
/opt/bin/cmake --version > /out/cmake-version.txt
dpkg-query -W > /out/native-builder-packages.txt
BUILD

FROM ${CUDA_RUNTIME_IMAGE} AS runtime
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ENV LD_LIBRARY_PATH=/usr/local/lib/whisper:/usr/local/cuda/lib64:/usr/local/nvidia/lib:/usr/local/nvidia/lib64
ARG CURL_VERSION
ARG CA_CERTIFICATES_VERSION
ARG APT_RETRIES
ARG OPENSSL_VERSION=3.0.13-0ubuntu3.16
ARG GNUPG_VERSION=2.4.4-2ubuntu17.6
ARG GCC_RUNTIME_VERSION=14.2.0-4ubuntu2~24.04.1
ARG CUDA_TOOLKIT_VERSION
ARG CUDA_EULA_SHA256
ARG FETCH_CONNECT_TIMEOUT
ARG FETCH_MAX_TIME
ARG FETCH_RETRIES
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

rm -f /etc/apt/sources.list.d/cuda*.list /etc/apt/sources.list.d/nvidia*.list
apt-get -o APT::Update::Error-Mode=any -o Acquire::Retries="${APT_RETRIES}" update
apt-get install -y --no-install-recommends \
    "ca-certificates=${CA_CERTIFICATES_VERSION}" \
    "curl=${CURL_VERSION}" \
    "dirmngr=${GNUPG_VERSION}" \
    "gnupg=${GNUPG_VERSION}" \
    "gnupg-utils=${GNUPG_VERSION}" \
    "gnupg2=${GNUPG_VERSION}" \
    "gpg=${GNUPG_VERSION}" \
    "gpg-agent=${GNUPG_VERSION}" \
    "gpgconf=${GNUPG_VERSION}" \
    "gpgsm=${GNUPG_VERSION}" \
    "gpgv=${GNUPG_VERSION}" \
    "keyboxd=${GNUPG_VERSION}" \
    "libgomp1=${GCC_RUNTIME_VERSION}" \
    "libssl3t64=${OPENSSL_VERSION}" \
    "libstdc++6=${GCC_RUNTIME_VERSION}" \
    "openssl=${OPENSSL_VERSION}"
rm -rf /var/lib/apt/lists/*

groupadd --gid "${RUNTIME_GID}" whisper
useradd --uid "${RUNTIME_UID}" --gid "${RUNTIME_GID}" \
    --no-create-home --shell /usr/sbin/nologin whisper

mkdir -p /etc/whisper /models /tmp/whisper-backend /usr/local/share/whisper/licenses/cuda
chown "${RUNTIME_UID}:${RUNTIME_GID}" /models /tmp/whisper-backend
chmod 0700 /tmp/whisper-backend

cp /NGC-DL-CONTAINER-LICENSE /usr/local/share/whisper/licenses/cuda/
curl -fsSL \
    --connect-timeout "${FETCH_CONNECT_TIMEOUT}" \
    --max-time "${FETCH_MAX_TIME}" \
    --retry "${FETCH_RETRIES}" \
    "https://docs.nvidia.com/cuda/archive/${CUDA_TOOLKIT_VERSION}/eula/index.html" \
    --output /usr/local/share/whisper/licenses/cuda/EULA.html
printf '%s  %s\n' "${CUDA_EULA_SHA256}" /usr/local/share/whisper/licenses/cuda/EULA.html | sha256sum -c -
dpkg-query -W > /usr/local/share/whisper/runtime-packages.txt
BUILD

COPY --link --from=go-build /out/whisper-backend /usr/local/bin/whisper-backend
COPY --link --from=go-build /out/licenses /usr/local/share/whisper/licenses
COPY --link --from=go-build \
    /out/go-build.txt \
    /out/go-modules.txt \
    /out/go-sources.txt \
    /out/go-sources.sha256 \
    /usr/local/share/whisper/
COPY --link --from=native-build /out/bin/whisper-server /usr/local/bin/whisper-server
COPY --link --from=native-build /out/lib/ /usr/local/lib/whisper/
COPY --link --from=native-build /out/licenses/ /usr/local/share/whisper/licenses/
COPY --link --from=native-build \
    /out/native-build-flags.txt \
    /out/cuda-compiler.txt \
    /out/cmake-version.txt \
    /out/native-builder-packages.txt \
    /usr/local/share/whisper/
COPY --link containers/build-inputs.json /usr/local/share/whisper/build-inputs.json

ARG GO_VERSION
ARG CUDA_BUILD_IMAGE
ARG CUDA_RUNTIME_IMAGE
ARG CUDA_ARCHITECTURES
ARG WHISPER_REVISION
ARG WHISPER_COMMIT
ARG WHISPER_SHA256
ARG WHISPER_SERVER_BLOB
ARG CMAKE_VERSION
ARG CMAKE_SHA256
RUN <<BUILD
cat > /usr/local/share/whisper/build.json <<JSON
{
  "go_version": "${GO_VERSION}",
  "gpu_build": false,
  "upstream_revision": "${WHISPER_REVISION}",
  "upstream_commit": "${WHISPER_COMMIT}",
  "server_blob": "${WHISPER_SERVER_BLOB}",
  "upstream_sha256": "${WHISPER_SHA256}",
  "cuda_builder": "${CUDA_BUILD_IMAGE}",
  "runtime_base": "${CUDA_RUNTIME_IMAGE}",
  "cuda_architectures": "${CUDA_ARCHITECTURES}",
  "ggml_native": false,
  "cmake_version": "${CMAKE_VERSION}",
  "cmake_sha256": "${CMAKE_SHA256}"
}
JSON
ldd /usr/local/bin/whisper-server > /usr/local/share/whisper/linked-libraries.txt
awk '/not found/ && $1 != "libcuda.so.1" {bad=1} END {exit bad}' /usr/local/share/whisper/linked-libraries.txt
BUILD

ARG SOURCE_REVISION=unreleased
ARG APP_VERSION=0.0.0-dev
ARG IMAGE_SOURCE
ARG IMAGE_TITLE=openwhispr-whisper-server-backend
ARG IMAGE_DESCRIPTION="Resident Whisper supervisor and unchanged CUDA native server"
ARG IMAGE_AUTHORS="openwhispr-whisper-server contributors"
ARG IMAGE_VENDOR=${IMAGE_AUTHORS}
ARG IMAGE_URL=${IMAGE_SOURCE}
ARG IMAGE_DOCUMENTATION=${IMAGE_SOURCE}/blob/main/docs/container-builds.md
ARG IMAGE_LICENSES="Apache-2.0 AND MIT AND BSD-3-Clause AND LicenseRef-NVIDIA-CUDA"
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
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD ["curl", "--fail", "--silent", "--max-time", "2", "--output", "/dev/null", "http://127.0.0.1:8081/health"]
ENTRYPOINT ["/usr/local/bin/whisper-backend"]
