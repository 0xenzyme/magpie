FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -tags nogui -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" -o /out/magpie . \
    && mkdir /config

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/magpie /magpie
COPY --from=build --chown=65532:65532 /config /config

ENV XDG_CONFIG_HOME=/config \
    MAGPIE_ADDR=0.0.0.0:3425

VOLUME /config

EXPOSE 3425 3430

ENTRYPOINT ["/magpie"]
CMD ["serve"]
