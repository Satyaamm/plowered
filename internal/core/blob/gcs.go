package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	gcstorage "cloud.google.com/go/storage"
)

// GCS is an ObjectStore backed by Google Cloud Storage. Credentials
// resolve through Application Default Credentials — GOOGLE_APPLICATION_
// CREDENTIALS service-account JSON, workload identity on GKE, or the
// metadata server on GCE / Cloud Run. No bespoke credential plumbing.
type GCS struct {
	client *gcstorage.Client
	bucket string
}

// GCSConfig is the constructor input. Bucket is required; auth is
// always ADC, so there's nothing else to configure.
type GCSConfig struct {
	Bucket string
}

// NewGCS wires the GCS SDK. The client is lazy about credentials —
// a misconfigured ADC chain surfaces on first use, not here — so the
// boot path should log success only after a probe if it needs proof.
func NewGCS(ctx context.Context, cfg GCSConfig) (*GCS, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("blob/gcs: bucket name required")
	}
	client, err := gcstorage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("blob/gcs: client: %w", err)
	}
	return &GCS{client: client, bucket: cfg.Bucket}, nil
}

func (g *GCS) Put(ctx context.Context, key string, body io.Reader) (string, error) {
	w := g.client.Bucket(g.bucket).Object(key).NewWriter(ctx)
	if _, err := io.Copy(w, body); err != nil {
		_ = w.Close()
		return "", fmt.Errorf("blob/gcs: put %s: %w", key, err)
	}
	// Close commits the object; most write errors surface here.
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("blob/gcs: put %s: %w", key, err)
	}
	return "gs://" + g.bucket + "/" + key, nil
}

func (g *GCS) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := g.client.Bucket(g.bucket).Object(key).NewReader(ctx)
	if err != nil {
		if errors.Is(err, gcstorage.ErrObjectNotExist) {
			return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
		}
		return nil, fmt.Errorf("blob/gcs: get %s: %w", key, err)
	}
	return r, nil
}

func (g *GCS) Delete(ctx context.Context, key string) error {
	err := g.client.Bucket(g.bucket).Object(key).Delete(ctx)
	if err != nil {
		// Idempotent like the other backends.
		if errors.Is(err, gcstorage.ErrObjectNotExist) {
			return nil
		}
		return fmt.Errorf("blob/gcs: delete %s: %w", key, err)
	}
	return nil
}

func (g *GCS) SignedURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	// V4 signing. With a service-account JSON the client signs
	// locally; with workload identity it calls the IAM signBlob API
	// (the SA needs roles/iam.serviceAccountTokenCreator on itself).
	u, err := g.client.Bucket(g.bucket).SignedURL(key, &gcstorage.SignedURLOptions{
		Method:  "GET",
		Expires: time.Now().Add(ttl),
		Scheme:  gcstorage.SigningSchemeV4,
	})
	if err != nil {
		return "", fmt.Errorf("blob/gcs: sign %s: %w", key, err)
	}
	return u, nil
}
