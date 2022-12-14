# Multipart to S3 (mps3)

[![CI](https://github.com/gabrielhora/mps3/actions/workflows/main.yml/badge.svg)](https://github.com/gabrielhora/mps3/actions/workflows/main.yml)

Save uploaded files directly to an S3 compatible API.

## Use case

Your app accepts user uploaded files and you need to process those files later in a pool of workers. Instead of saving uploaded files to the web server's tmp and later uploading to a shared staging area, this middleware allows you to upload directly to said staging area. It doesn't allocate disk space in the web server, it uploads to S3 as it reads from the request body.

## Example

```go
package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/gabrielhora/mps3"
)

func main() {
	server := http.NewServeMux()

	server.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// no need to call `req.ParseMultipartForm()` or `req.ParseForm()`, the middleware
		// will process the data, upload the files and return the following information.

		// suppose a multipart form was posted with fields "file" and "name".
		fileKey := req.Form.Get("file")                                    // "<field>" contains the S3 key where this file was saved
		fileName := req.Form.Get("file_name")                              // "<field>_name" is the original uploaded file name
		fileType := req.Form.Get("file_type")                              // "<field>_type" contains the file content type
		fileSize, _ := strconv.ParseInt(req.Form.Get("file_size"), 10, 64) // "<field>_size" is the file size

		name := req.Form.Get("name") // other fields are accessed normally

		// ...
	}))
	
	s3cfg, _ := config.LoadDefaultConfig(context.Background())

	s3, err := mps3.New(mps3.Config{
		// S3Config you can specify static credentials or custom endpoints if needed.
		// By default if not specified the middleware you load the default configuration.
		S3Config: &s3cfg,

		// ACL used for the bucket when CreateBucket is true
		BucketACL: "private",

		// If true it will try to create the bucket, if the bucket already exists and
		// it belongs to the same account ("BucketAlreadyOwnedByYou") it won't do anything
		CreateBucket: true,

		// ACL used for uploaded files
		FileACL: "private",

		// A logger that is used to print out error messages during request handling
		Logger: log.Default(),

		// Size of the upload chunk to S3 (minimum is 5MB)
		PartSize: 1024 * 1024 * 5,

		// Function called for uploaded files to determine their S3 key prefix.
		// This is the default implementation.
		PrefixFunc: func(req *http.Request) string {
			return time.Now().UTC().Format("/2006/01/02/")
		},
	})
	if err != nil {
		// handle error
	}

	// To use it, just wrap the Handler (either the whole server or specific routes)
	_ = http.ListenAndServe(":8080", s3.Wrap(server))
}
```
