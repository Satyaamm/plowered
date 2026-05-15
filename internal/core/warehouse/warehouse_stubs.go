package warehouse

import "context"

// NotInstalledExecutor is the placeholder used until a cloud driver's
// real implementation lands. Its Query always returns
// ErrDriverNotInstalled. Used for BigQuery + Athena + DynamoDB +
// MongoDB at the moment — wiring real drivers means swapping the
// factory registration in cmd/plowered/main.go, no other code changes.
type NotInstalledExecutor struct{ Type string }

func (n NotInstalledExecutor) Query(_ context.Context, _ string) (Rows, error) {
	return nil, ErrDriverNotInstalled
}
