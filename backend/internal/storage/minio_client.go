package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	LinkPayBucket = "linkpay"
	MaxFileSize   = 2 * 1024 * 1024 * 1024 // 2GB
)

// allowedMIMEs define os tipos de arquivo aceitos no upload de produtos digitais.
var allowedMIMEs = map[string]bool{
	"application/pdf":       true,
	"image/jpeg":            true,
	"image/png":             true,
	"image/gif":             true,
	"application/zip":       true,
	"video/mp4":             true,
	"audio/mpeg":            true,
	"application/epub+zip":  true,
	"application/x-zip-compressed": true,
}

// MinIOClient encapsula o cliente MinIO com operações comuns ao LinkPay.
type MinIOClient struct {
	client   *minio.Client
	endpoint string
}

// NewMinIOClient cria um cliente MinIO a partir de variáveis de ambiente.
// Retorna nil, nil se MINIO_ENDPOINT não estiver configurado (uploads serão rejeitados via IsConfigured).
func NewMinIOClient() (*MinIOClient, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT") // ex: "192.168.30.121:9000"
	if endpoint == "" {
		return nil, nil
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	useSSL := os.Getenv("MINIO_USE_SSL") == "true"

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: NewClient: %w", err)
	}
	return &MinIOClient{client: client, endpoint: endpoint}, nil
}

// IsConfigured indica se o cliente MinIO está disponível.
func (m *MinIOClient) IsConfigured() bool {
	return m != nil && m.client != nil
}

// EnsureBucket cria o bucket se não existir.
func (m *MinIOClient) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := m.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("minio: BucketExists: %w", err)
	}
	if !exists {
		if err := m.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("minio: MakeBucket: %w", err)
		}
	}
	return nil
}

// UploadFile faz upload de um arquivo para o MinIO.
// Retorna erro se o tipo não for permitido ou o tamanho exceder 2GB.
func (m *MinIOClient) UploadFile(ctx context.Context, bucket, storageKey, mimeType string, reader io.Reader, size int64) error {
	if !allowedMIMEs[mimeType] {
		return fmt.Errorf("tipo de arquivo não permitido: %s", mimeType)
	}
	if size > MaxFileSize {
		return fmt.Errorf("arquivo muito grande: máximo 2GB")
	}
	_, err := m.client.PutObject(ctx, bucket, storageKey, reader, size, minio.PutObjectOptions{
		ContentType: mimeType,
	})
	if err != nil {
		return fmt.Errorf("minio: PutObject: %w", err)
	}
	return nil
}

// PresignedURL gera URL temporária para download direto (válida por 15 minutos).
func (m *MinIOClient) PresignedURL(ctx context.Context, bucket, storageKey string) (string, error) {
	u, err := m.client.PresignedGetObject(ctx, bucket, storageKey, 15*time.Minute, nil)
	if err != nil {
		return "", fmt.Errorf("minio: PresignedGetObject: %w", err)
	}
	return u.String(), nil
}

// GetObject retorna um reader para download direto do objeto do MinIO.
func (m *MinIOClient) GetObject(ctx context.Context, bucket, storageKey string) (*minio.Object, error) {
	obj, err := m.client.GetObject(ctx, bucket, storageKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio: GetObject: %w", err)
	}
	return obj, nil
}

// DeleteFile remove um arquivo do MinIO.
func (m *MinIOClient) DeleteFile(ctx context.Context, bucket, storageKey string) error {
	return m.client.RemoveObject(ctx, bucket, storageKey, minio.RemoveObjectOptions{})
}
