FROM golang:1.21 AS builder
RUN mkdir /project
WORKDIR /project

COPY go.mod .
COPY go.sum .
RUN go mod download

ARG PLUGIN
RUN test -n "$PLUGIN" || ( echo "Plugin not specified. Aborting" && false )

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$(go env GOARCH) go build -ldflags="-s -w" ./cmd/$PLUGIN

FROM alpine:3.19

ARG PLUGIN

COPY --from=builder /project/$PLUGIN /usr/bin/liqo-resource-plugin

ENTRYPOINT [ "/usr/bin/liqo-resource-plugin" ]