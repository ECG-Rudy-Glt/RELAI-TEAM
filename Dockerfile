# syntax=docker/dockerfile:1
FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/relai-server ./cmd/relai-server

# Runtime needs git (every repo is checked with it) plus the two CLIs the aggregator reads
# through: bd (Beads) and openspec. Without them a repo still shows up, with a note saying
# which part could not be read.
FROM node:22-bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @beads/bd @fission-ai/openspec \
 && npm cache clean --force \
 && git config --system --add safe.directory '*'
COPY --from=build /out/relai-server /usr/local/bin/relai-server
ENV RELAI_TEAM_CONFIG=/config/config.json
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["relai-server"]
