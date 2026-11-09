FROM golang:1.27.3-alpine AS build
ARG XRAY_VERSION=26.11.9
ARG XRAY_COMMIT=676eaf66f8f05d4d558ee7a74b4736623d3feeb9
ARG RULES_DAT=20261108215333
ARG GEOIP_SHA256=f067a385d9b0f10b71790ad1c3524c7fe1c87e7b8a460f099b529919d8a2c50a
ARG GEOSITE_SHA256=62c396669ab4898be650c5ef8da20630055d1adeea838f7fededd1e4e0510d8a
ENV GOTOOLCHAIN=local
RUN apk add --no-cache git
RUN git clone --quiet --depth 1 --branch "v${XRAY_VERSION}" https://github.com/XTLS/Xray-core /src \
	&& commit=$(git -C /src rev-parse HEAD) \
	&& if [ "$commit" != "$XRAY_COMMIT" ]; then echo "tag v${XRAY_VERSION} points to ${commit}, want ${XRAY_COMMIT}" >&2; exit 1; fi
WORKDIR /src
RUN CGO_ENABLED=0 go build -o /out/xray -trimpath -buildvcs=false -gcflags="all=-l=4" -ldflags "-s -w -buildid=" ./main
RUN mkdir -p /out/empty /out/geodat \
	&& wget -q -O /out/geodat/geoip.dat "https://github.com/XTLS/Xray-rules-dat/releases/download/${RULES_DAT}/geoip-full.dat" \
	&& wget -q -O /out/geodat/geosite.dat "https://github.com/XTLS/Xray-rules-dat/releases/download/${RULES_DAT}/geosite-expand.dat" \
	&& printf '%s  %s\n%s  %s\n' "$GEOIP_SHA256" /out/geodat/geoip.dat "$GEOSITE_SHA256" /out/geodat/geosite.dat | sha256sum -c -

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=0:0 --chmod=755 /out/xray /usr/local/bin/xray
COPY --from=build --chown=65532:65532 --chmod=755 /out/empty /usr/local/share/xray
COPY --from=build --chown=65532:65532 --chmod=644 /out/geodat/geoip.dat /out/geodat/geosite.dat /usr/local/share/xray/
ENV TZ=Etc/UTC
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/xray"]
CMD ["run", "-c", "/etc/xray/config.json"]
