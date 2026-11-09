FROM golang:1.27.3-alpine AS caps
RUN apk add --no-cache libcap
COPY bin/sylph-agent bin/agent.test /out/sylph/
RUN chown -R 65532:65532 /out \
	&& setcap cap_kill,cap_net_admin+ep /out/sylph/sylph-agent \
	&& setcap cap_kill,cap_net_admin+ep /out/sylph/agent.test

FROM ghcr.io/itg-labs/sylph-xray:26.11.9
COPY --from=caps /out/sylph/ /sylph/
COPY --chown=65532:65532 bin/xray/ /etc/xray/
ENTRYPOINT []
