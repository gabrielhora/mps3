# MultiPart to S3 (mps3)

Save uploaded files directly to an AWS S3 (or compatible) bucket.

## Usage

```go
import (
    "net/http"
    "log"
    "github.com/gabrielhora/mps3"
)

func main() {
    server := http.NewServerMux()

    server.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// no need to call `req.ParseMultipartForm()` or `req.ParseForm()`, the middleware
		// will process the data, upload the files and return the following information.

		// suppose a multipart form was uploaded with fields named "file" and "name".
        file_key := req.Form.Get("file")  // "<field>" contains the S3 key where this file was saved
		file_name := req.Form.Get("file_name")  // "<field>_name" is the original uploaded file name
		file_type := req.Form.Get("file_type")  // "<field>_type" contains the file content type
		file_size, _ := strconv.ParseInt(req.Form.Get("file_size"), 10, 64) // "<field>_size" is the file size

        name := req.Form.Get("name")  // other fields are accessed normally

        // ...
	}))

    s3, err := mps3.New(mps3.Config{
		AccessKeyID:     "access_key_id",
		SecretAccessKey: "secret_access_key",
		Endpoint:        "api_endpoint",
		Region:          "region",
		Bucket:          "bucket",

        // ACL used for the bucket when CreateBucket is true
		BucketACL: "private",

        // If true it will try to create the bucket, if the bucket already exists and
        // it belongs to the same account ("BucketAlreadyOwnedByYou") it won't do anything
		CreateBucket: true,

        // ACL used for uploaded files
		FileACL: "private",

        // A logger that is used to print out error messages during request handling
		Logger: log.Default(),

        // Function called for uploaded files to determine their S3 key prefix.
        // This is the default implementation.
		PrefixFunc: func(req *http.Request) string {
			return time.Now().UTC().Format("2006/01/02/")
		},
	})
    if err != nil {
        // handle error
    }

    // To use it, just wrap the Handler
    _ = http.ListenAndServe(":8080", s3.Wrap(server))
}
```
