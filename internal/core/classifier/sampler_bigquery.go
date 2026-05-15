package classifier

import (
	"context"
	"errors"
)

// ErrDriverNotInstalled is returned by warehouse samplers whose driver
// is excluded from the build. v0 ships without the BigQuery client (no
// stable cgo-free option) so attempting to classify a BigQuery
// connection surfaces this error end-to-end. Callers should render it
// as a friendly "BigQuery classification is not available in this
// build" message rather than a 500.
var ErrDriverNotInstalled = errors.New("classifier: warehouse driver not installed in this build")

// BigQuerySampler is a placeholder so the dispatcher can satisfy the
// Sampler interface for BigQuery connections. It returns
// ErrDriverNotInstalled for every call.
type BigQuerySampler struct{}

func (BigQuerySampler) SampleTable(
	_ context.Context,
	_, _, _, _ string,
	_ []string,
) ([]Result, error) {
	return nil, ErrDriverNotInstalled
}
