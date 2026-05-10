FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/plowered-mcp ./cmd/plowered-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/plowered-mcp /usr/local/bin/plowered-mcp
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/plowered-mcp"]
