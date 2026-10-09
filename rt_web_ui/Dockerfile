# rt_web_ui image: the criteria UI (serve) by default; send works too.
# git.frontiir.net modules are private, so the build uses vendor/ made on the
# host (make docker-build runs go mod vendor first) instead of fetching them.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY *.go criteria_ui.html ./
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/rt-web-ui .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/rt-web-ui /app/rt-web-ui
COPY criteria/ /app/criteria/
COPY ticket_valid.json /app/
ENTRYPOINT ["/app/rt-web-ui"]
CMD ["serve", "--env", "/app/.env", "--addr", "127.0.0.1:8090", "--criteria", "/app/criteria/tickets"]
