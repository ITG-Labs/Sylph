FROM ghcr.io/xtls/xray-core:26.10.10
COPY --chown=65532:65532 bin/sylph-agent bin/agent.test /sylph/
COPY --chown=65532:65532 bin/xray/ /etc/xray/
ENTRYPOINT []
