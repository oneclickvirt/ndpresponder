FROM golang:1.26-alpine AS build
WORKDIR /app
COPY . .
RUN env CGO_ENABLED=0 GOBIN=/build go install .

FROM scratch
COPY --from=build /build/* /
ENTRYPOINT ["/ndpresponder"]
