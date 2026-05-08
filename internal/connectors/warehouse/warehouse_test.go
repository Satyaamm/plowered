package warehouse_test

import (
	"context"
	"testing"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/connectors/warehouse"
)

func TestInfo(t *testing.T) {
	c := &warehouse.Connector{}
	info := c.Info()
	if info.Name != warehouse.ConnectorName {
		t.Errorf("name = %q", info.Name)
	}
	if !info.SupportsLineage {
		t.Error("expected SupportsLineage = true")
	}
	if len(info.SupportedAssetTypes) < 5 {
		t.Errorf("supported types = %v", info.SupportedAssetTypes)
	}
}

func TestValidateRequiresDSN(t *testing.T) {
	c := &warehouse.Connector{}
	if err := c.Validate(context.Background(), shared.Config{}); err == nil {
		t.Error("expected error when dsn missing")
	}
}

func TestRegistration(t *testing.T) {
	c, err := shared.Default.Build(warehouse.ConnectorName)
	if err != nil {
		t.Fatalf("registry build: %v", err)
	}
	if c.Info().Name != warehouse.ConnectorName {
		t.Errorf("info name = %q", c.Info().Name)
	}
}
