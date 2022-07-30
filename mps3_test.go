package mps3

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
)

var cfg = s3Config()
var s3cli = s3.NewFromConfig(*cfg)

const bucket = "test"

func TestUploadFilesToS3(t *testing.T) {
	assert := assert.New(t)

	data := map[string]string{
		"name": "Gabriel",
	}
	req, err := newRequest(data, "test_file1.png", "test_file2.txt")
	assert.NoError(err)
	res := httptest.NewRecorder()

	wrapper, err := New(Config{
		S3Config:     cfg,
		Bucket:       bucket,
		CreateBucket: true,
	})
	assert.NoError(err)

	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(2, len(req.Form["file"]))

		assert.True(existInS3(req.Form["file"][0]))
		assert.True(existInS3(req.Form["file"][1]))

		assert.Equal("test_file1.png", req.Form["file_name"][0])
		assert.Equal("15716", req.Form["file_size"][0])
		assert.Equal("image/png", req.Form["file_type"][0])

		assert.Equal("test_file2.txt", req.Form["file_name"][1])
		assert.Equal("12", req.Form["file_size"][1])
		assert.Equal("text/plain; charset=utf-8", req.Form["file_type"][1])

		assert.Equal("Gabriel", req.Form.Get("name"))
	})
	wrapper.Wrap(h).ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
}

func newRequest(fields map[string]string, files ...string) (*http.Request, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)
	defer writer.Close()

	for _, fname := range files {
		err := func() error {
			f, err := os.Open(fname)
			if err != nil {
				return err
			}
			defer f.Close()

			part, err := writer.CreateFormFile("file", filepath.Base(fname))
			if err != nil {
				return err
			}
			_, err = io.Copy(part, f)
			if err != nil {
				return err
			}

			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	req := httptest.NewRequest("POST", "/", buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

func existInS3(name string) bool {
	_, err := s3cli.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(name),
	})
	return err == nil
}

func s3Config() *aws.Config {
	host := os.Getenv("MINIO_HOST")
	if host == "" {
		host = "http://localhost:9000"
	}
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               host,
			SigningRegion:     "localhost",
			HostnameImmutable: true,
		}, nil
	})
	cfg, _ := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("minioadmin", "minioadmin", "")),
		config.WithEndpointResolverWithOptions(resolver))
	return &cfg
}
