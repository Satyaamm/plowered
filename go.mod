module github.com/Satyaamm/plowered

go 1.23

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/jackc/pgx/v5 v5.7.2
	golang.org/x/time v0.7.0
	google.golang.org/grpc v1.69.2
	google.golang.org/protobuf v1.36.1
)

// `go mod tidy` populates transitive deps and creates go.sum on first build.
