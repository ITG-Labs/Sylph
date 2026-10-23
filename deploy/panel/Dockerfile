FROM node:26-alpine AS web
WORKDIR /src/services/panel/web
COPY services/panel/web/package.json services/panel/web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY services/panel/web/ ./
RUN npm run build

FROM golang:1.27.3-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY internal ./internal
COPY pkg ./pkg
COPY services/panel ./services/panel
COPY --from=web /src/services/panel/ui/dist ./services/panel/ui/dist
WORKDIR /src/services/panel
RUN GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/ITG-Labs/sylph/internal/version.Version=${VERSION}" -o /out/sylph-panel ./cmd/panel

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sylph-panel /sylph-panel
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/sylph-panel"]
