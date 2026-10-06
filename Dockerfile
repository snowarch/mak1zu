FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /mak1zu ./cmd/mak1zu

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /mak1zu /mak1zu
WORKDIR /data
VOLUME /data
# Inside a container the panel must bind 0.0.0.0, which requires web_ui.token; publish it on 127.0.0.1 only.
ENTRYPOINT ["/mak1zu", "-c", "/data/config.json"]
CMD ["run"]
