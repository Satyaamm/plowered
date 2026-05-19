package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyerr "github.com/aws/smithy-go"
)

// S3 is an ObjectStore backed by Amazon S3 (and compatible
// implementations — MinIO, Cloudflare R2, Backblaze B2 — via
// BaseEndpoint). The constructor pulls credentials from the standard
// AWS resolution chain (env vars, ~/.aws/credentials, IRSA, EC2
// instance profile). Region must be supplied; bucket is implicit per
// store instance so caller paths don't carry it.
type S3 struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

// S3Config is the constructor input. Region + Bucket are required;
// every other field is optional with sensible AWS defaults.
type S3Config struct {
	// Bucket name. Required.
	Bucket string
	// Region (e.g. "us-east-1"). Required for the SDK; even
	// S3-compatible backends accept it.
	Region string
	// BaseEndpoint overrides the S3 endpoint URL. Set this for MinIO
	// ("http://localhost:9000") or R2. Leave empty for real AWS.
	BaseEndpoint string
	// UsePathStyle forces path-style addressing
	// ("https://endpoint/bucket/key" instead of
	// "https://bucket.endpoint/key"). Required for MinIO; usually
	// false for real AWS.
	UsePathStyle bool
}

// NewS3 wires the AWS SDK. Returns an error if config load fails or
// the bucket name is empty.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("blob/s3: bucket name required")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("blob/s3: load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.BaseEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.BaseEndpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	return &S3{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
	}, nil
}

func (s *S3) Put(ctx context.Context, key string, body io.Reader) (string, error) {
	// PutObject requires an io.ReadSeeker for chunked signing. The SDK
	// will buffer when given a plain Reader; that's acceptable for
	// the small artifacts this package handles (JSON profiles, AI
	// logs). If we start storing multi-GB files we'd switch to the
	// manager.Uploader for true streaming.
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		return "", fmt.Errorf("blob/s3: put %s: %w", key, err)
	}
	return "s3://" + s.bucket + "/" + key, nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
		}
		return nil, fmt.Errorf("blob/s3: get %s: %w", key, err)
	}
	return out.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// S3 DeleteObject is idempotent at the API level — it doesn't
		// error on missing keys. We catch only real failures.
		return fmt.Errorf("blob/s3: delete %s: %w", key, err)
	}
	return nil
}

func (s *S3) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("blob/s3: presign %s: %w", key, err)
	}
	return req.URL, nil
}

// isS3NotFound matches both the typed NoSuchKey error and the more
// generic 404 wrapper the SDK sometimes returns.
func isS3NotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var apiErr smithyerr.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchKey" {
		return true
	}
	return false
}
