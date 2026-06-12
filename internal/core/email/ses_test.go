package email

import (
	"context"
	"testing"
)

func TestNewSESSenderRequiresRegion(t *testing.T) {
	if _, err := NewSESSender(context.Background(), ""); err == nil {
		t.Fatal("want error when region missing")
	}
}
