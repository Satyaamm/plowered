FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/plowered ./cmd/plowered

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/plowered /usr/local/bin/plowered
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/plowered"]
CMD ["serve"]
