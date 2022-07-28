package mps3

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

var cfg = Config{
	AccessKeyID:     "minioadmin",
	SecretAccessKey: "minioadmin",
	Endpoint:        "http://localhost:9000",
	Region:          "localhost",
	Bucket:          "test",
	CreateBucket:    true,
}

func TestUploadFiles(t *testing.T) {
	assert := assert.New(t)

	data := map[string]string{
		"name": "Gabriel",
	}
	req, err := newRequest(data, "test_file1.png", "test_file2.txt")
	assert.NoError(err)
	res := httptest.NewRecorder()

	s3, err := New(cfg)
	assert.NoError(err)

	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(2, len(req.Form["file"]))

		assert.Equal("test_file1.png", req.Form["file_name"][0])
		assert.Equal("15716", req.Form["file_size"][0])
		assert.Equal("image/png", req.Form["file_type"][0])

		assert.Equal("test_file2.txt", req.Form["file_name"][1])
		assert.Equal("12", req.Form["file_size"][1])
		assert.Equal("text/plain; charset=utf-8", req.Form["file_type"][1])

		assert.Equal("Gabriel", req.Form.Get("name"))
	})
	s3.Wrap(h).ServeHTTP(res, req)

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