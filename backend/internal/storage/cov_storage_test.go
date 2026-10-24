package storage

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// covClosedEndpoint is an address that refuses connections immediately so
// MinIO calls fail fast without any external network dependency.
const covClosedEndpoint = "127.0.0.1:1"

// covClient builds a MinIOClient pointed at an unreachable endpoint.
func covClient(t *testing.T) *MinIOClient {
	t.Helper()
	t.Setenv("MINIO_ENDPOINT", covClosedEndpoint)
	t.Setenv("MINIO_ACCESS_KEY", "cov-access")
	t.Setenv("MINIO_SECRET_KEY", "cov-secret")
	t.Setenv("MINIO_USE_SSL", "false")

	c, err := NewMinIOClient()
	if err != nil {
		t.Fatalf("NewMinIOClient returned error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client for configured endpoint")
	}
	return c
}

func covCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}

func TestCovNewMinIOClient_NoEndpoint(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "")

	c, err := NewMinIOClient()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil client, got %+v", c)
	}
}

func TestCovNewMinIOClient_WithEndpoint(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "127.0.0.1:9000")
	t.Setenv("MINIO_ACCESS_KEY", "access")
	t.Setenv("MINIO_SECRET_KEY", "secret")
	t.Setenv("MINIO_USE_SSL", "true")

	c, err := NewMinIOClient()
	if err != nil {
		t.Fatalf("NewMinIOClient returned error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.client == nil {
		t.Fatal("expected underlying minio client")
	}
	if c.endpoint != "127.0.0.1:9000" {
		t.Fatalf("unexpected endpoint %q", c.endpoint)
	}
	if !c.IsConfigured() {
		t.Fatal("expected configured client")
	}
}

func TestCovNewMinIOClient_InvalidEndpoint(t *testing.T) {
	// minio.New rejects endpoints that carry a scheme.
	t.Setenv("MINIO_ENDPOINT", "http://127.0.0.1:9000")

	c, err := NewMinIOClient()
	if err == nil {
		t.Fatalf("expected error for endpoint with scheme, got client %+v", c)
	}
	if !strings.Contains(err.Error(), "minio: NewClient") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovIsConfigured_NilClient(t *testing.T) {
	var m *MinIOClient
	if m.IsConfigured() {
		t.Fatal("nil MinIOClient must not be configured")
	}
}

func TestCovIsConfigured_ZeroValue(t *testing.T) {
	m := &MinIOClient{}
	if m.IsConfigured() {
		t.Fatal("zero-value MinIOClient must not be configured")
	}
}

func TestCovIsConfigured_Configured(t *testing.T) {
	m := covClient(t)
	if !m.IsConfigured() {
		t.Fatal("client with underlying minio client must be configured")
	}
}

func TestCovUploadFile_DisallowedMIME(t *testing.T) {
	// No underlying client needed: validation happens before any network use.
	m := &MinIOClient{}
	err := m.UploadFile(context.Background(), "bucket", "key", "text/plain", strings.NewReader("x"), 1)
	if err == nil {
		t.Fatal("expected error for disallowed MIME type")
	}
	if !strings.Contains(err.Error(), "não permitido") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovUploadFile_TooLarge(t *testing.T) {
	m := &MinIOClient{}
	err := m.UploadFile(context.Background(), "bucket", "key", "application/pdf", strings.NewReader("x"), MaxFileSize+1)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "muito grande") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovUploadFile_NetworkError(t *testing.T) {
	m := covClient(t)
	ctx, cancel := covCtx()
	defer cancel()

	start := time.Now()
	err := m.UploadFile(ctx, "linkpay", "k.pdf", "application/pdf", bytes.NewReader([]byte("%PDF-1.4")), 8)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected network error for closed endpoint")
	}
	if !strings.Contains(err.Error(), "PutObject") {
		t.Fatalf("expected wrapped PutObject error, got %q", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("network error took too long: %v", elapsed)
	}
}

func TestCovEnsureBucket_ClosedEndpoint(t *testing.T) {
	m := covClient(t)
	ctx, cancel := covCtx()
	defer cancel()

	err := m.EnsureBucket(ctx, LinkPayBucket)
	if err == nil {
		t.Fatal("expected error for closed endpoint")
	}
	if !strings.Contains(err.Error(), "BucketExists") && !strings.Contains(err.Error(), "MakeBucket") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovPresignedURL_ClosedEndpoint(t *testing.T) {
	m := covClient(t)
	ctx, cancel := covCtx()
	defer cancel()

	url, err := m.PresignedURL(ctx, LinkPayBucket, "some/key.pdf")
	if err == nil {
		t.Fatalf("expected error for closed endpoint, got URL %q", url)
	}
	if url != "" {
		t.Fatalf("expected empty URL on error, got %q", url)
	}
	if !strings.Contains(err.Error(), "PresignedGetObject") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovGetObject_ClosedEndpointOrReadError(t *testing.T) {
	m := covClient(t)
	ctx, cancel := covCtx()
	defer cancel()

	obj, err := m.GetObject(ctx, LinkPayBucket, "some/key.pdf")
	if err != nil {
		// Accept an eager error; it must be wrapped by the production code.
		if !strings.Contains(err.Error(), "GetObject") {
			t.Fatalf("unexpected error: %q", err)
		}
		return
	}
	if obj == nil {
		t.Fatal("expected non-nil object when no eager error is returned")
	}
	defer obj.Close()

	buf := make([]byte, 32)
	if _, readErr := obj.Read(buf); readErr == nil {
		t.Fatal("expected read error from object on closed endpoint")
	}
}

func TestCovGetObject_InvalidBucketName(t *testing.T) {
	m := covClient(t)
	ctx, cancel := covCtx()
	defer cancel()

	obj, err := m.GetObject(ctx, "INVALID_BUCKET!", "key")
	if err == nil {
		if obj != nil {
			_ = obj.Close()
		}
		t.Fatal("expected error for invalid bucket name")
	}
	if !strings.Contains(err.Error(), "GetObject") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovDeleteFile_ClosedEndpoint(t *testing.T) {
	m := covClient(t)
	ctx, cancel := covCtx()
	defer cancel()

	if err := m.DeleteFile(ctx, LinkPayBucket, "some/key.pdf"); err == nil {
		t.Fatal("expected error for closed endpoint")
	}
}

func TestCovAllowedMIMEs(t *testing.T) {
	allowed := []string{
		"application/pdf",
		"image/jpeg",
		"image/png",
		"image/gif",
		"application/zip",
		"video/mp4",
		"audio/mpeg",
		"application/epub+zip",
		"application/x-zip-compressed",
	}
	for _, mime := range allowed {
		if !allowedMIMEs[mime] {
			t.Errorf("expected %q to be allowed", mime)
		}
	}

	denied := []string{
		"text/plain",
		"text/html",
		"application/x-msdownload",
		"application/octet-stream",
		"",
	}
	for _, mime := range denied {
		if allowedMIMEs[mime] {
			t.Errorf("expected %q to be denied", mime)
		}
	}
}
