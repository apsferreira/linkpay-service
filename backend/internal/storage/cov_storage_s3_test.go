package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// covFakeS3 is a minimal S3-compatible endpoint sufficient for the MinIO
// client operations used by MinIOClient (bucket lookup/create, single PUT,
// GetBucketLocation). It keeps every assertion offline and deterministic.
type covFakeS3 struct {
	mu            sync.Mutex
	headStatus    int
	putBucketCode int
	requests      []string
}

const covLocationXML = `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`

const covErrorXML = `<?xml version="1.0" encoding="UTF-8"?><Error><Code>InternalError</Code><Message>boom</Message><Resource>/%s/</Resource><RequestId>1</RequestId></Error>`

func (f *covFakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Query().Has("location"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(covLocationXML))
	case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/"):
		w.WriteHeader(f.headStatus)
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/"):
		// MakeBucket
		if f.putBucketCode != 0 && f.putBucketCode != http.StatusOK {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(f.putBucketCode)
			_, _ = w.Write([]byte(strings.ReplaceAll(covErrorXML, "%s", strings.Trim(r.URL.Path, "/"))))
			return
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut:
		// PutObject (single part)
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// covClientForServer points a MinIOClient at the given fake server.
func covClientForServer(t *testing.T, srv *httptest.Server) *MinIOClient {
	t.Helper()
	endpoint := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("MINIO_ENDPOINT", endpoint)
	t.Setenv("MINIO_ACCESS_KEY", "cov-access")
	t.Setenv("MINIO_SECRET_KEY", "cov-secret")
	t.Setenv("MINIO_USE_SSL", "false")

	c, err := NewMinIOClient()
	if err != nil {
		t.Fatalf("NewMinIOClient returned error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	return c
}

func TestCovEnsureBucket_ExistingBucket(t *testing.T) {
	fake := &covFakeS3{headStatus: http.StatusOK}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	m := covClientForServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.EnsureBucket(ctx, LinkPayBucket); err != nil {
		t.Fatalf("EnsureBucket returned error: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, req := range fake.requests {
		if strings.HasPrefix(req, http.MethodPut) {
			t.Fatalf("MakeBucket should not be called for an existing bucket, requests=%v", fake.requests)
		}
	}
}

func TestCovEnsureBucket_CreatesMissingBucket(t *testing.T) {
	fake := &covFakeS3{headStatus: http.StatusNotFound, putBucketCode: http.StatusOK}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	m := covClientForServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.EnsureBucket(ctx, "cov-new-bucket"); err != nil {
		t.Fatalf("EnsureBucket returned error: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	created := false
	for _, req := range fake.requests {
		if req == "PUT /cov-new-bucket/" {
			created = true
		}
	}
	if !created {
		t.Fatalf("expected MakeBucket request, got %v", fake.requests)
	}
}

func TestCovEnsureBucket_MakeBucketError(t *testing.T) {
	// 400 is not retried by the MinIO client, keeping the test fast.
	fake := &covFakeS3{headStatus: http.StatusNotFound, putBucketCode: http.StatusBadRequest}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	m := covClientForServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := m.EnsureBucket(ctx, "cov-failing-bucket")
	if err == nil {
		t.Fatal("expected MakeBucket error")
	}
	if !strings.Contains(err.Error(), "MakeBucket") {
		t.Fatalf("expected wrapped MakeBucket error, got %q", err)
	}
}

func TestCovUploadFile_Success(t *testing.T) {
	fake := &covFakeS3{headStatus: http.StatusOK}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	m := covClientForServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := []byte("%PDF-1.4 cov")
	if err := m.UploadFile(ctx, LinkPayBucket, "cov/key.pdf", "application/pdf", strings.NewReader(string(payload)), int64(len(payload))); err != nil {
		t.Fatalf("UploadFile returned error: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	found := false
	for _, req := range fake.requests {
		if req == "PUT /linkpay/cov/key.pdf" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PutObject request, got %v", fake.requests)
	}
}

func TestCovPresignedURL_Success(t *testing.T) {
	fake := &covFakeS3{headStatus: http.StatusOK}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	m := covClientForServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url, err := m.PresignedURL(ctx, LinkPayBucket, "cov/key.pdf")
	if err != nil {
		t.Fatalf("PresignedURL returned error: %v", err)
	}
	if !strings.Contains(url, "X-Amz-Signature=") {
		t.Fatalf("expected presigned query params, got %q", url)
	}
	if !strings.Contains(url, "/linkpay/cov/key.pdf") {
		t.Fatalf("expected bucket/key in URL, got %q", url)
	}
}
