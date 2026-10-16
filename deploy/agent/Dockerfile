FROM golang:1.27.2-alpine AS build
ARG VERSION=dev
RUN apk add --no-cache libcap
WORKDIR /src
COPY go.mod ./
COPY internal ./internal
COPY pkg ./pkg
COPY services/agent ./services/agent
WORKDIR /src/services/agent
RUN GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/ITG-Labs/sylph/internal/version.Version=${VERSION}" -o /out/sylph-agent ./cmd/agent \
	&& setcap cap_kill+ep /out/sylph-agent
RUN install -d -m 0750 -o 65533 -g 65532 /out/root/etc/xray /out/root/usr/local/share/xray/sylph \
	&& install -d -m 0700 -o 65533 -g 65532 /out/root/var/lib/sylph-agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sylph-agent /sylph-agent
COPY --from=build /out/root/ /
ENV SYLPH_CACHE_PATH=/var/lib/sylph-agent/cache.json \
	SYLPH_STATS_PATH=/var/lib/sylph-agent/stats.json \
	SYLPH_NODE_STATE_PATH=/var/lib/sylph-agent/node.json \
	SYLPH_GEO_STATE_PATH=/var/lib/sylph-agent/geo.json
USER 65533:65532
ENTRYPOINT ["/sylph-agent"]
