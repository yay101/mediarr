package s3

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	awsTypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	partSize           = 100 * 1024 * 1024 // 100MB part size
	multiPartThreshold = 100 * 1024 * 1024 // 100MB threshold for multipart
)

type BackendType string

const TypeS3 BackendType = "s3"

type Reader interface {
	Type() string
	Write(ctx context.Context, key string, reader io.Reader, size int64) error
	Read(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	GetSize(ctx context.Context, key string) (int64, error)
	Close() error
}

type S3Backend struct {
	client         *s3.Client
	bucket         string
	forcePathStyle bool
	mu             sync.Mutex
}

type Options struct {
	Bucket         string
	Region         string
	Endpoint       string
	AccessKey      string
	SecretKey      string
	ForcePathStyle bool
}

func New(opts Options) (*S3Backend, error) {
	var region string
	if opts.Region != "" {
		region = opts.Region
	} else if opts.Endpoint != "" {
		parts := strings.Split(strings.TrimPrefix(opts.Endpoint, "https://"), ".")
		if len(parts) >= 2 && len(parts[0]) > 3 && strings.HasSuffix(parts[0], "-") {
			region = strings.TrimSuffix(parts[0], "-")
		}
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			opts.AccessKey,
			opts.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, err
	}

	var endpoint *string
	if opts.Endpoint != "" {
		endpoint = aws.String(opts.Endpoint)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = endpoint
		o.UsePathStyle = opts.ForcePathStyle
	})

	return &S3Backend{
		client:         client,
		bucket:         opts.Bucket,
		forcePathStyle: opts.ForcePathStyle,
	}, nil
}

func (s *S3Backend) Type() string {
	return string(TypeS3)
}

func (s *S3Backend) Write(ctx context.Context, key string, reader io.Reader, size int64) error {
	if size == 0 {
		info, err := reader.(*os.File).Stat()
		if err == nil {
			size = info.Size()
		}
	}

	if size > multiPartThreshold {
		return s.writeMultipart(ctx, key, reader, size)
	}

	return s.writeSimple(ctx, key, reader)
}

func (s *S3Backend) writeSimple(ctx context.Context, key string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return err
	}

	return s.verifyUpload(ctx, key, int64(len(data)))
}

func (s *S3Backend) writeMultipart(ctx context.Context, key string, reader io.Reader, size int64) error {
	uploadID, err := s.startMultipart(ctx, key)
	if err != nil {
		return err
	}

	completedParts := make([]awsTypes.CompletedPart, 0)
	partNum := 0
	uploaded := int64(0)
	buf := make([]byte, partSize)

	for {
		select {
		case <-ctx.Done():
			s.abortMultipart(ctx, key, uploadID)
			return ctx.Err()
		default:
		}

		n, err := reader.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			s.abortMultipart(ctx, key, uploadID)
			return err
		}

		partNum++
		etag, err := s.uploadPart(ctx, key, uploadID, partNum, buf[:n])
		if err != nil {
			s.abortMultipart(ctx, key, uploadID)
			return err
		}

		completedParts = append(completedParts, awsTypes.CompletedPart{
			ETag:       etag,
			PartNumber: aws.Int32(int32(partNum)),
		})
		uploaded += int64(n)

		slog.Debug("upload progress", "key", key, "uploaded", uploaded, "total", size, "percent", float64(uploaded)*100/float64(size))
	}

	if err := s.completeMultipart(ctx, key, uploadID, completedParts); err != nil {
		return err
	}

	return s.verifyUpload(ctx, key, size)
}

func (s *S3Backend) startMultipart(ctx context.Context, key string) (string, error) {
	resp, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}
	return *resp.UploadId, nil
}

func (s *S3Backend) uploadPart(ctx context.Context, key, uploadID string, partNum int, data []byte) (*string, error) {
	resp, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		UploadId:      aws.String(uploadID),
		PartNumber:    aws.Int32(int32(partNum)),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return nil, err
	}
	return resp.ETag, nil
}

func (s *S3Backend) completeMultipart(ctx context.Context, key string, uploadID string, parts []awsTypes.CompletedPart) error {
	_, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &awsTypes.CompletedMultipartUpload{Parts: parts},
	})
	return err
}

func (s *S3Backend) abortMultipart(ctx context.Context, key, uploadID string) {
	_, _ = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
}

func (s *S3Backend) verifyUpload(ctx context.Context, key string, expectedSize int64) error {
	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}

	if *resp.ContentLength != expectedSize {
		return &VerifyError{
			expected: expectedSize,
			actual:   *resp.ContentLength,
			key:      key,
		}
	}

	slog.Debug("S3 upload verified", "key", key, "size", expectedSize)
	return nil
}

type VerifyError struct {
	expected int64
	actual   int64
	key      string
}

func (e *VerifyError) Error() string {
	return "S3 upload verification failed: size mismatch for " + e.key
}

func (s *S3Backend) Read(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (s *S3Backend) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *S3Backend) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *awsTypes.NotFound
		if ok := isNotFound(err, &notFound); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func isNotFound(err error, target **awsTypes.NotFound) bool {
	ok := false
	switch err.(type) {
	case *awsTypes.NotFound:
		ok = true
	}
	return ok
}

func (s *S3Backend) GetSize(ctx context.Context, key string) (int64, error) {
	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, err
	}
	return *resp.ContentLength, nil
}

func (s *S3Backend) Close() error {
	return nil
}

var _ Reader = (*S3Backend)(nil)

type Backend = Reader
