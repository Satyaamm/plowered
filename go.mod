module github.com/Satyaamm/plowered

go 1.23

require (
	github.com/jackc/pgx/v5 v5.7.2
)

// `go mod tidy` will populate transitive deps and create go.sum on first build.
// M2+ adds: gRPC, grpc-gateway, protovalidate, JWT, NATS, Bleve.
