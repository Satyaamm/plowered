module github.com/Satyaamm/plowered

go 1.23

// Dependencies added per milestone — run `go mod tidy` after first build to
// populate go.sum. M1 needs only the standard library + uuid (added when
// proto codegen lands). Heavier deps (pgx, jwt, grpc) come with M1+M2.
