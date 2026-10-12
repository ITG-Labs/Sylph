FROM caddy:2.11.7-builder AS build
RUN xcaddy build v2.11.7 --with github.com/mholt/caddy-l4@v0.1.2

FROM caddy:2.11.7
COPY --from=build /usr/bin/caddy /usr/bin/caddy
COPY caddy.json /etc/caddy/caddy.json
RUN mkdir -p /run/caddy-admin
CMD ["caddy", "run", "--config", "/etc/caddy/caddy.json"]
