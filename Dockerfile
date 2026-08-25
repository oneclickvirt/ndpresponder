# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /app
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN target_arch="${TARGETARCH:-$(go env GOARCH)}"; \
    env CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="$target_arch" \
        go build -trimpath -o /out/ndpresponder .

FROM scratch
COPY --from=build /out/ndpresponder /ndpresponder
ENTRYPOINT ["/ndpresponder"]
