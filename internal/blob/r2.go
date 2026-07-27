package blob

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type R2Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

type R2 struct {
	bucket string
	client *s3.Client
}

func NewR2(ctx context.Context, config R2Config) (*R2, error) {
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(config.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(config.Endpoint)
		options.UsePathStyle = true
	})
	return &R2{bucket: config.Bucket, client: client}, nil
}

func (r *R2) Put(ctx context.Context, key, mediaType string, size int64, body io.Reader) error {
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket), Key: aws.String(key), Body: body,
		ContentLength: aws.Int64(size), ContentType: aws.String(mediaType),
	})
	if err != nil {
		return fmt.Errorf("upload R2 object %s: %w", key, err)
	}
	return nil
}

func (r *R2) Delete(ctx context.Context, key string) error {
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete R2 object %s: %w", key, err)
	}
	return nil
}

// PresignGet issues a short-lived download URL for a private object.
func (r *R2) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	request, err := s3.NewPresignClient(r.client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket), Key: aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign R2 object %s: %w", key, err)
	}
	return request.URL, nil
}
