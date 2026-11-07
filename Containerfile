# Build tsgit on the native build platform and cross-compile for the target
# image architecture. Only the runtime stage below is architecture-specific.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/tsgit ./cmd/tsgit

FROM alpine:3.24

ARG VERSION=dev

# git is the serving backend and ca-certificates are required to reach the
# Tailscale control plane over HTTPS.
RUN apk add --no-cache ca-certificates git \
    && addgroup -S -g 10001 tsgit \
    && adduser -S -D -H -u 10001 -G tsgit -h /var/lib/tsgit tsgit \
    && install -d -m 0750 -o tsgit -g tsgit /var/lib/tsgit \
    && install -d -m 0700 -o tsgit -g tsgit /var/lib/tsgit/state \
    && install -d -m 0750 -o tsgit -g tsgit /var/lib/tsgit/repos \
    && install -d -m 0700 -o tsgit -g tsgit /var/lib/tsgit/tmp

COPY --from=build /out/tsgit /usr/local/bin/tsgit

ENV TSGIT_STATE_DIR=/var/lib/tsgit/state \
    TSGIT_REPO_ROOT=/var/lib/tsgit/repos \
    HOME=/var/lib/tsgit \
    TMPDIR=/var/lib/tsgit/tmp

LABEL org.opencontainers.image.source="https://github.com/kevindurb/tsgit" \
      org.opencontainers.image.version="${VERSION}"

USER 10001:10001

# State and repositories are separate on purpose: the state directory holds
# the node key and is a secret, while the repository root is the durable data.
VOLUME ["/var/lib/tsgit/state", "/var/lib/tsgit/repos"]

ENTRYPOINT ["/usr/local/bin/tsgit"]
CMD ["serve"]
