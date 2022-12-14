package mps3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/h2non/filetype"
)

type Logger interface {
	Printf(format string, args ...any)
}

type Config struct {
	// S3Config specifies credentials and endpoint configuration. If not specified the middleware
	// will load the default configuration with a background context.
	//
	// To provide a custom endpoint (required when not using AWS S3 API) you can do something like this
	// (more info at https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/endpoints/):
	//
	//	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
	//		if service == s3.ServiceID {
	//			return aws.Endpoint{
	//				URL:               "http://localhost:9000",
	//				SigningRegion:     "eu-central-1",
	//				HostnameImmutable: true,
	//			}, nil
	//		}
	//		// returning EndpointNotFoundError will allow the service to fallback to it's default resolution
	//		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	//	})
	//
	//	s3cfg, err := config.LoadDefaultConfig(context.Background(), config.WithEndpointResolverWithOptions(resolver))
	S3Config *aws.Config

	// Bucket name of the bucket to use to store uploaded files
	Bucket string

	// BucketACL if CreateBucket is true the bucket will be created with this ACL (default: "private")
	BucketACL string

	// CreateBucket if true it will try to create a bucket with the specified Bucket name.
	// Error of type BucketAlreadyOwnedByYou will be silently ignored (default: true)
	CreateBucket bool

	// FileACL defines ACL string to use for uploaded files (default: "private")
	FileACL string

	// PartSize defines the size of the chunk that is uploaded to S3, by default is 5 MB,
	// which is the minimum part size. If a value smaller than the minimum is set, it
	// will be silently adjusted to the minimum.
	PartSize int64

	// PrefixFunc defines a function that gets executed to define the S3 key prefix
	// for each uploaded file. By default it's a function that returns the current date
	// in the format `/YYYY/MM/DD/`
	PrefixFunc func(*http.Request) string

	// Logger is used to log errors during request processing (default: log.Default())
	Logger Logger
}

type Wrapper struct {
	uploader   *manager.Uploader
	logger     Logger
	bucket     string
	fileACL    string
	prefixFunc func(*http.Request) string
}

type file struct {
	name  string
	ftype string
	key   string
	size  int64
}

func New(cfg Config) (*Wrapper, error) {
	if cfg.S3Config == nil {
		s3cfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 configuration: %w", err)
		}
		cfg.S3Config = &s3cfg
	}

	cli := s3.NewFromConfig(*cfg.S3Config)

	if cfg.Bucket == "" {
		return nil, fmt.Errorf("bucket name is required")
	}
	if cfg.CreateBucket {
		if cfg.BucketACL == "" {
			cfg.BucketACL = "private"
		}
		if err := createBucket(cli, cfg.Bucket, cfg.BucketACL); err != nil {
			return nil, err
		}
	}

	if cfg.PartSize < manager.MinUploadPartSize {
		cfg.PartSize = manager.MinUploadPartSize
	}

	w := Wrapper{
		uploader: manager.NewUploader(cli, func(u *manager.Uploader) {
			u.PartSize = cfg.PartSize
		}),
		logger:     cfg.Logger,
		bucket:     cfg.Bucket,
		fileACL:    cfg.FileACL,
		prefixFunc: cfg.PrefixFunc,
	}
	if w.logger == nil {
		w.logger = log.Default()
	}
	if w.fileACL == "" {
		w.fileACL = "private"
	}
	if w.prefixFunc == nil {
		w.prefixFunc = func(*http.Request) string {
			return time.Now().UTC().Format("/2006/01/02/")
		}
	}

	return &w, nil
}

func (wr Wrapper) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data") {
			next.ServeHTTP(w, req)
			return
		}

		mr, err := req.MultipartReader()
		if err != nil {
			wr.logAndErr(w, fmt.Errorf("failed create multipart reader: %w", err))
			return
		}

		f := make(url.Values)
		for {
			part, err := mr.NextPart()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				wr.logAndErr(w, fmt.Errorf("failed to read request part: %w", err))
				return
			}

			if err := wr.readPart(req, part, f); err != nil {
				wr.logAndErr(w, err)
				return
			}
		}

		if req.Form == nil {
			req.Form = make(url.Values)
		}
		if req.PostForm == nil {
			req.PostForm = make(url.Values)
		}
		for k, v := range f {
			req.PostForm[k] = append(req.PostForm[k], v...)
			req.Form[k] = append(req.Form[k], v...)
		}

		next.ServeHTTP(w, req)
	})
}

func (wr Wrapper) readPart(req *http.Request, part *multipart.Part, frm url.Values) error {
	defer func() {
		if err := part.Close(); err != nil {
			wr.logger.Printf("failed to close part: %v", err)
		}
	}()

	name := part.FormName()

	// read file

	if part.FileName() != "" {
		f, err := wr.readFile(req, part)
		if err != nil {
			return err
		}

		// if couldn't find type based on file header, try based on extension
		if f.ftype == "application/octet-stream" {
			if t := mime.TypeByExtension(filepath.Ext(f.name)); t != "" {
				f.ftype = t
			}
		}
		frm[name] = append(frm[name], f.key)
		frm[name+"_name"] = append(frm[name+"_name"], f.name)
		frm[name+"_type"] = append(frm[name+"_type"], f.ftype)
		frm[name+"_size"] = append(frm[name+"_size"], fmt.Sprintf("%d", f.size))
		return nil
	}

	// read string

	val, err := wr.readString(part)
	if err != nil {
		return err
	}
	frm[name] = append(frm[name], val)
	return nil
}

func (wr Wrapper) readFile(req *http.Request, part *multipart.Part) (file, error) {
	f := file{
		name: filepath.Clean(part.FileName()),
		key:  wr.prefixFunc(req) + uuid.NewString(),
	}

	counter := &bytesCounter{r: part}
	_, err := wr.uploader.Upload(req.Context(), &s3.PutObjectInput{
		ACL:    types.ObjectCannedACL(wr.fileACL),
		Key:    aws.String(f.key),
		Body:   counter,
		Bucket: aws.String(wr.bucket),
	})
	if err != nil {
		return file{}, fmt.Errorf("failed to upload file to S3: %w", err)
	}

	f.size = counter.count
	f.ftype = counter.fileType

	return f, nil
}

func (Wrapper) readString(p *multipart.Part) (string, error) {
	buf := bytes.Buffer{}
	if _, err := buf.ReadFrom(p); err != nil {
		return "", fmt.Errorf("failed to read string part: %w", err)
	}
	return buf.String(), nil
}

func createBucket(cli *s3.Client, name, acl string) error {
	_, err := cli.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String(name),
		ACL:    types.BucketCannedACL(acl),
	})
	if err != nil {
		var aerr *types.BucketAlreadyOwnedByYou
		if errors.As(err, &aerr) {
			return nil
		}
		return fmt.Errorf("failed to create bucket %q: %w", name, err)
	}
	return nil
}

func (wr Wrapper) logAndErr(w http.ResponseWriter, err error) {
	wr.logger.Printf("failed to read request part: %v", err)
	http.Error(w, http.StatusText(500), 500)
}

type bytesCounter struct {
	r        io.Reader
	count    int64
	typeBuf  []byte
	fileType string
}

func (bc *bytesCounter) Read(b []byte) (int, error) {
	n, err := bc.r.Read(b)
	bc.count += int64(n)

	// accumulate a few bytes (at most 261 according to https://github.com/h2non/filetype)
	// so we can try to detect the content type via the file header
	if bc.fileType == "" {
		bc.typeBuf = append(bc.typeBuf, b...)

		if errors.Is(err, io.EOF) || len(bc.typeBuf) >= 261 {
			t, err := filetype.Match(bc.typeBuf)
			if err != nil || t.MIME.Value == "" {
				bc.fileType = "application/octet-stream"
			} else {
				bc.fileType = t.MIME.Value
			}
			bc.typeBuf = nil
		}
	}

	return n, err
}
