# syntax=docker/dockerfile:1

# Build stages run on the build machine and cross-compile, so multi-arch
# images need no emulation except for the final apk install.

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# Writes ../internal/webui/dist, which the Go binary embeds.
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/parsec ./cmd/parsec

FROM alpine:3.24
# git does the storage and sync; ssh is for pushing to an SSH remote.
RUN apk add --no-cache git openssh-client ca-certificates \
 && adduser -D -u 10001 -h /home/parsec parsec \
 && mkdir -p /data /state \
 && chown parsec:parsec /data /state
COPY --from=server /out/parsec /usr/local/bin/parsec
COPY deploy/parsec.docker.yaml /etc/parsec/parsec.yaml
# Commits are authored by whoever made the change; this is only the committer.
ENV GIT_COMMITTER_NAME=parsec \
    GIT_COMMITTER_EMAIL=parsec@users.parsec.invalid
USER parsec
VOLUME ["/data", "/state"]
EXPOSE 7343
ENTRYPOINT ["parsec"]
CMD ["--config", "/etc/parsec/parsec.yaml"]
