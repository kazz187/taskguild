package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Storage implements Storage using AWS S3.
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Storage creates a new S3Storage.
func NewS3Storage(ctx context.Context, bucket, prefix, region string) (*S3Storage, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &S3Storage{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: strings.TrimSuffix(prefix, "/") + "/",
	}, nil
}

func (s *S3Storage) key(path string) string {
	return s.prefix + strings.TrimPrefix(path, "/")
}

func (s *S3Storage) Read(ctx context.Context, path string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(path)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if isNoSuchKey(err, nsk) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to read s3://%s/%s: %w", s.bucket, s.key(path), err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body of s3://%s/%s: %w", s.bucket, s.key(path), err)
	}
	return data, nil
}

func (s *S3Storage) Write(ctx context.Context, path string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(path)),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("failed to write s3://%s/%s: %w", s.bucket, s.key(path), err)
	}
	return nil
}

func (s *S3Storage) Delete(ctx context.Context, path string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(path)),
	})
	if err != nil {
		return fmt.Errorf("failed to delete s3://%s/%s: %w", s.bucket, s.key(path), err)
	}
	return nil
}

func (s *S3Storage) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := s.key(prefix)
	if !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}
	out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.bucket),
		Prefix:    aws.String(fullPrefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list s3://%s/%s: %w", s.bucket, fullPrefix, err)
	}
	var paths []string
	for _, obj := range out.Contents {
		rel := strings.TrimPrefix(aws.ToString(obj.Key), s.prefix)
		paths = append(paths, rel)
	}
	return paths, nil
}

func (s *S3Storage) Exists(ctx context.Context, path string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(path)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if isNoSuchKey(err, nsk) {
			return false, nil
		}
		// HeadObject returns NotFound differently
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check existence of s3://%s/%s: %w", s.bucket, s.key(path), err)
	}
	return true, nil
}

func isNoSuchKey(err error, target *types.NoSuchKey) bool {
	// Simple error type check - the AWS SDK v2 uses specific error types
	var nsk *types.NoSuchKey
	return strings.Contains(err.Error(), "NoSuchKey") || nsk != nil
}
