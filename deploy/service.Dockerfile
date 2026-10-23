FROM golang:1.27.3-alpine AS build
ARG SERVICE
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY internal ./internal
COPY pkg ./pkg
COPY services/${SERVICE} ./services/${SERVICE}
WORKDIR /src/services/${SERVICE}
RUN GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/ITG-Labs/sylph/internal/version.Version=${VERSION}" -o /out/sylph ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sylph /sylph
USER nonroot:nonroot
ENTRYPOINT ["/sylph"]
