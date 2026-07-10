# Build the standalone awareness-graph gRPC server. Build context is the repo
# root:  docker build -f deploy/Dockerfile .
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off => a static binary that runs on distroless. The embedded seed
# (golang/server/embeddata) is compiled in via go:embed.
RUN CGO_ENABLED=0 go build -trimpath -o /out/awareness-graph ./golang/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/awareness-graph /awareness-graph
# The server connects to an external Oxigraph (see docker-compose.yml) and seeds
# it from the embedded corpus on first start (seedIfEmpty). Bearer-token auth is
# opt-in via $AWG_TOKEN.
EXPOSE 10120
ENTRYPOINT ["/awareness-graph"]
CMD ["-addr", ":10120", "-oxigraph-url", "http://oxigraph:7878/query", "-require-store"]
