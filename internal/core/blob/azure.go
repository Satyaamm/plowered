package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

// AzureBlob is an ObjectStore backed by Azure Blob Storage. Two auth
// modes, picked by which config field is set:
//
//   - ConnectionString — account-key auth. Simplest for dev and for
//     customers who hand us a connection string. SAS URLs are signed
//     with the shared key.
//   - Account — AAD auth via DefaultAzureCredential (env vars,
//     workload identity, managed identity, az CLI). The production
//     posture: no long-lived keys. SAS URLs are signed with a
//     user-delegation key fetched on demand.
//
// Container plays the role S3's bucket does; keys keep the same
// "<tenant>/<feature>/<id>/<file>" convention.
type AzureBlob struct {
	client    *azblob.Client
	container string
	// sharedKey is true under connection-string auth, where SAS
	// signing uses the account key instead of a user delegation key.
	sharedKey bool
}

// AzureBlobConfig is the constructor input. Container is required,
// plus exactly one of ConnectionString or Account.
type AzureBlobConfig struct {
	// Container name. Required.
	Container string
	// ConnectionString from the storage account's "Access keys" blade.
	// Mutually exclusive with Account.
	ConnectionString string
	// Account is the storage account name (https://<account>.blob.
	// core.windows.net). Auth resolves via DefaultAzureCredential.
	Account string
}

// NewAzureBlob wires the Azure SDK. Returns an error when the config
// is incomplete or the credential chain can't initialise.
func NewAzureBlob(_ context.Context, cfg AzureBlobConfig) (*AzureBlob, error) {
	if cfg.Container == "" {
		return nil, errors.New("blob/azure: container name required")
	}
	switch {
	case cfg.ConnectionString != "":
		client, err := azblob.NewClientFromConnectionString(cfg.ConnectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("blob/azure: connection string client: %w", err)
		}
		return &AzureBlob{client: client, container: cfg.Container, sharedKey: true}, nil
	case cfg.Account != "":
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("blob/azure: default credential: %w", err)
		}
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.Account)
		client, err := azblob.NewClient(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("blob/azure: client: %w", err)
		}
		return &AzureBlob{client: client, container: cfg.Container}, nil
	default:
		return nil, errors.New("blob/azure: either ConnectionString or Account required")
	}
}

func (a *AzureBlob) Put(ctx context.Context, key string, body io.Reader) (string, error) {
	// UploadStream buffers + parallelises internally; defaults are
	// fine for the JSON-sized artifacts this package carries.
	if _, err := a.client.UploadStream(ctx, a.container, key, body, nil); err != nil {
		return "", fmt.Errorf("blob/azure: put %s: %w", key, err)
	}
	return "azblob://" + a.container + "/" + key, nil
}

func (a *AzureBlob) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := a.client.DownloadStream(ctx, a.container, key, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound, bloberror.ContainerNotFound) {
			return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
		}
		return nil, fmt.Errorf("blob/azure: get %s: %w", key, err)
	}
	return resp.Body, nil
}

func (a *AzureBlob) Delete(ctx context.Context, key string) error {
	_, err := a.client.DeleteBlob(ctx, a.container, key, nil)
	if err != nil {
		// Idempotent like the S3 backend: a missing blob is success.
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil
		}
		return fmt.Errorf("blob/azure: delete %s: %w", key, err)
	}
	return nil
}

func (a *AzureBlob) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	start := time.Now().UTC().Add(-5 * time.Minute) // clock-skew cushion
	expiry := time.Now().UTC().Add(ttl)
	blobClient := a.client.ServiceClient().
		NewContainerClient(a.container).
		NewBlobClient(key)

	if a.sharedKey {
		// Account-key SAS — the blob client holds the shared key.
		u, err := blobClient.GetSASURL(sas.BlobPermissions{Read: true}, expiry, nil)
		if err != nil {
			return "", fmt.Errorf("blob/azure: sas %s: %w", key, err)
		}
		return u, nil
	}

	// AAD path — fetch a user delegation key, then sign with it. The
	// key fetch is one API call per SignedURL; callers issue these
	// rarely (downloads of DSR exports + profile snapshots), so we
	// don't cache.
	const iso = "2006-01-02T15:04:05Z"
	startISO, expiryISO := start.Format(iso), expiry.Format(iso)
	udc, err := a.client.ServiceClient().GetUserDelegationCredential(ctx, service.KeyInfo{
		Start:  &startISO,
		Expiry: &expiryISO,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("blob/azure: user delegation key: %w", err)
	}
	qp, err := sas.BlobSignatureValues{
		Protocol:      sas.ProtocolHTTPS,
		StartTime:     start,
		ExpiryTime:    expiry,
		Permissions:   (&sas.BlobPermissions{Read: true}).String(),
		ContainerName: a.container,
		BlobName:      key,
	}.SignWithUserDelegation(udc)
	if err != nil {
		return "", fmt.Errorf("blob/azure: sign %s: %w", key, err)
	}
	return blobClient.URL() + "?" + qp.Encode(), nil
}
