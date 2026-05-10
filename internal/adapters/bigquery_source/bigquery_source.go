// Package bigquery_source is the Plowered adapter for customer-owned
// BigQuery datasources.
//
// BigQuery does not expose a database/sql driver — its Go client is a
// REST/gRPC API surface. The full implementation needs:
//
//	import "cloud.google.com/go/bigquery"
//
// To keep the codebase compiling without that dependency until the
// first paying customer asks for BQ, the adapter ships in two modes:
//
//   - **Stub mode (default)**: Tester returns ErrDriverNotInstalled with
//     instructions; Crawler returns the same. The connection wizard
//     still lists BigQuery as an option so we can collect customer
//     configs ahead of enabling.
//   - **Active mode**: callers set `Active = bigqueryDriver` from a
//     plug-in package (e.g. internal/adapters/bigquery_driver/) that
//     does the heavy lifting against cloud.google.com/go/bigquery.
//
// Config shape (JSON):
//
//	{
//	  "project_id":  "acme-warehouse",
//	  "dataset":     "analytics",      // optional; crawl scopes to it
//	  "location":    "US",              // optional override
//	  "auth_method": "service_account"  // or "workload_identity"
//	}
//
// The secret bytes are interpreted as a service-account JSON key when
// auth_method is "service_account"; ignored for workload identity.
package bigquery_source

import (
	"context"
	"errors"
	"fmt"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/crawler"
)

// ErrDriverNotInstalled is returned by Tester / Crawler in stub mode.
// Operators should add the BigQuery driver dependency and register an
// Active driver to enable real connections.
var ErrDriverNotInstalled = errors.New(
	"bigquery driver not installed — add cloud.google.com/go/bigquery to go.mod " +
		"and register an Active driver via bigquery_source.SetActive()")

// Driver is the surface a plug-in implementation supplies. Kept
// minimal so the upgrade path is "write one struct, call SetActive".
type Driver interface {
	Test(ctx context.Context, projectID, location, authMethod string, serviceAccountJSON []byte) error
	Crawl(ctx context.Context, projectID, dataset, location, authMethod string, serviceAccountJSON []byte) (*crawler.Tree, error)
}

var active Driver

// SetActive registers a real BigQuery driver. Called once from the cmd
// binary if the deployment ships the cloud.google.com/go/bigquery dep.
func SetActive(d Driver) { active = d }

// Tester satisfies connection.Tester. Delegates to the active driver
// when registered, otherwise returns ErrDriverNotInstalled.
type Tester struct{}

func New() *Tester { return &Tester{} }

func (Tester) Test(ctx context.Context, cfg map[string]any, secret []byte) error {
	if active == nil {
		return ErrDriverNotInstalled
	}
	projectID, _ := cfg["project_id"].(string)
	if projectID == "" {
		return errors.New("bigquery_source: project_id is required")
	}
	location, _ := cfg["location"].(string)
	authMethod, _ := cfg["auth_method"].(string)
	if authMethod == "" {
		authMethod = "service_account"
	}
	return active.Test(ctx, projectID, location, authMethod, secret)
}

// Crawler satisfies crawler.Source. Same delegation pattern as Tester.
type Crawler struct{}

func NewCrawler() *Crawler { return &Crawler{} }

func (Crawler) Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*crawler.Tree, error) {
	if active == nil {
		return nil, ErrDriverNotInstalled
	}
	projectID, _ := cfg["project_id"].(string)
	if projectID == "" {
		return nil, errors.New("bigquery_source: project_id is required")
	}
	dataset, _ := cfg["dataset"].(string)
	location, _ := cfg["location"].(string)
	authMethod, _ := cfg["auth_method"].(string)
	if authMethod == "" {
		authMethod = "service_account"
	}
	return active.Crawl(ctx, projectID, dataset, location, authMethod, secret)
}

// ValidateConfig is what the connection HTTP create handler would call
// to bounce malformed configs before they even land in the DB. Not used
// in v0 (the handler trusts the wizard), kept here as the canonical
// shape spec.
func ValidateConfig(cfg map[string]any) error {
	if _, ok := cfg["project_id"].(string); !ok {
		return errors.New("project_id (string) is required")
	}
	if v, ok := cfg["auth_method"].(string); ok && v != "" {
		switch v {
		case "service_account", "workload_identity":
		default:
			return fmt.Errorf("auth_method must be service_account or workload_identity (got %q)", v)
		}
	}
	return nil
}

var _ connection.Tester = (*Tester)(nil)
var _ crawler.Source = (*Crawler)(nil)
