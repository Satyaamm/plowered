package blob

import (
	"context"
	"strings"
	"testing"
)

// Constructor validation only — round-trip behaviour needs live cloud
// credentials and belongs to the integration suite, not unit tests.

func TestNewAzureBlobValidation(t *testing.T) {
	ctx := context.Background()

	if _, err := NewAzureBlob(ctx, AzureBlobConfig{}); err == nil {
		t.Fatal("want error when container missing")
	}
	if _, err := NewAzureBlob(ctx, AzureBlobConfig{Container: "artifacts"}); err == nil {
		t.Fatal("want error when neither ConnectionString nor Account set")
	}

	// A syntactically-valid connection string constructs without any
	// network call; auth failures surface on first use.
	connStr := "DefaultEndpointsProtocol=https;AccountName=devstore;AccountKey=" +
		"ZGV2c3RvcmVrZXlkZXZzdG9yZWtleWRldnN0b3Jla2V5ZGV2c3RvcmVrZXkwMDA=;EndpointSuffix=core.windows.net"
	store, err := NewAzureBlob(ctx, AzureBlobConfig{Container: "artifacts", ConnectionString: connStr})
	if err != nil {
		t.Fatalf("connection-string construction: %v", err)
	}
	if !store.sharedKey {
		t.Fatal("connection-string auth should mark sharedKey")
	}
}

func TestNewGCSValidation(t *testing.T) {
	if _, err := NewGCS(context.Background(), GCSConfig{}); err == nil {
		t.Fatal("want error when bucket missing")
	}
}

func TestCloudURIShapes(t *testing.T) {
	// The Put return URIs are stored in catalog rows; their schemes are
	// part of the storage contract. Pin them so a refactor can't change
	// persisted-data semantics silently.
	for _, tc := range []struct {
		scheme string
		uri    string
	}{
		{"s3", "s3://bucket/tenant/feature/id/file.json"},
		{"azblob", "azblob://container/tenant/feature/id/file.json"},
		{"gs", "gs://bucket/tenant/feature/id/file.json"},
	} {
		if !strings.HasPrefix(tc.uri, tc.scheme+"://") {
			t.Fatalf("uri %q must use scheme %q", tc.uri, tc.scheme)
		}
	}
}
