FROM ghcr.io/sagernet/sing-box:v1.14.4 AS singbox

FROM metacubex/mihomo:v1.19.33
COPY --from=singbox /usr/local/bin/sing-box /usr/local/bin/sing-box
COPY bin/sylph-core bin/sylph-subs bin/subs.test /sylph/
ENTRYPOINT []
