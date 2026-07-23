FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sensei-github-app ./cmd/sensei-github-app

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sensei-github-app /sensei-github-app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/sensei-github-app"]
