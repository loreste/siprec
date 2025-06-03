package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/option"
)

// BackupStorage interface for different storage backends
type BackupStorage interface {
	Upload(localPath, backupID string) ([]string, error)
	Download(remotePath, localPath string) error
	List() ([]StoredBackup, error)
	Delete(remotePath string) error
	GetLocation() string
}

// StoredBackup represents a backup stored remotely
type StoredBackup struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	StorageType string    `json:"storage_type"`
	Encrypted   bool      `json:"encrypted"`
	Compressed  bool      `json:"compressed"`
}

// MultiBackupStorage manages multiple storage backends
type MultiBackupStorage struct {
	storages []BackupStorage
	logger   *logrus.Logger
}

// NewBackupStorage creates appropriate storage backends based on configuration
func NewBackupStorage(config StorageConfig, logger *logrus.Logger) (BackupStorage, error) {
	var storages []BackupStorage

	// Local storage (always enabled)
	if config.Local {
		localStorage := &LocalStorage{
			logger: logger,
		}
		storages = append(storages, localStorage)
	}

	// AWS S3 storage
	if config.S3.Enabled {
		s3Storage, err := NewS3Storage(config.S3, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize S3 storage: %w", err)
		}
		storages = append(storages, s3Storage)
	}

	// Google Cloud Storage
	if config.GCS.Enabled {
		gcsStorage, err := NewGCSStorage(config.GCS, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize GCS storage: %w", err)
		}
		storages = append(storages, gcsStorage)
	}

	// Azure Blob Storage
	if config.Azure.Enabled {
		azureStorage, err := NewAzureStorage(config.Azure, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Azure storage: %w", err)
		}
		storages = append(storages, azureStorage)
	}

	if len(storages) == 0 {
		return nil, fmt.Errorf("no storage backends configured")
	}

	return &MultiBackupStorage{
		storages: storages,
		logger:   logger,
	}, nil
}

// Upload uploads to all configured storage backends
func (m *MultiBackupStorage) Upload(localPath, backupID string) ([]string, error) {
	var locations []string
	var errors []string

	for _, storage := range m.storages {
		uploadLocations, err := storage.Upload(localPath, backupID)
		if err != nil {
			m.logger.WithError(err).WithField("storage", storage.GetLocation()).Warning("Failed to upload to storage backend")
			errors = append(errors, fmt.Sprintf("%s: %v", storage.GetLocation(), err))
		} else {
			locations = append(locations, uploadLocations...)
			m.logger.WithFields(logrus.Fields{
				"storage":   storage.GetLocation(),
				"locations": uploadLocations,
			}).Info("Successfully uploaded backup")
		}
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("failed to upload to any storage backend: %s", strings.Join(errors, "; "))
	}

	if len(errors) > 0 {
		m.logger.WithField("errors", errors).Warning("Some storage uploads failed")
	}

	return locations, nil
}

// Download downloads from the first available storage backend
func (m *MultiBackupStorage) Download(remotePath, localPath string) error {
	for _, storage := range m.storages {
		err := storage.Download(remotePath, localPath)
		if err == nil {
			return nil
		}
		m.logger.WithError(err).WithField("storage", storage.GetLocation()).Warning("Failed to download from storage backend")
	}
	return fmt.Errorf("failed to download from any storage backend")
}

// List returns backups from all storage backends
func (m *MultiBackupStorage) List() ([]StoredBackup, error) {
	var allBackups []StoredBackup

	for _, storage := range m.storages {
		backups, err := storage.List()
		if err != nil {
			m.logger.WithError(err).WithField("storage", storage.GetLocation()).Warning("Failed to list backups from storage backend")
			continue
		}
		allBackups = append(allBackups, backups...)
	}

	return allBackups, nil
}

// Delete deletes from all storage backends
func (m *MultiBackupStorage) Delete(remotePath string) error {
	var errors []string

	for _, storage := range m.storages {
		err := storage.Delete(remotePath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", storage.GetLocation(), err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to delete from some storage backends: %s", strings.Join(errors, "; "))
	}

	return nil
}

// GetLocation returns a description of all configured storage backends
func (m *MultiBackupStorage) GetLocation() string {
	var locations []string
	for _, storage := range m.storages {
		locations = append(locations, storage.GetLocation())
	}
	return strings.Join(locations, ", ")
}

// Local Storage Implementation

type LocalStorage struct {
	logger *logrus.Logger
}

func (l *LocalStorage) Upload(localPath, backupID string) ([]string, error) {
	// For local storage, the file is already in the correct location
	return []string{fmt.Sprintf("local://%s", localPath)}, nil
}

func (l *LocalStorage) Download(remotePath, localPath string) error {
	// Extract actual path from remote path (remove local:// prefix)
	actualPath := strings.TrimPrefix(remotePath, "local://")

	// Copy file if different locations
	if actualPath != localPath {
		return copyFile(actualPath, localPath)
	}

	return nil
}

func (l *LocalStorage) List() ([]StoredBackup, error) {
	backupDir := "/var/backups/siprec" // Default backup directory
	if envDir := os.Getenv("BACKUP_PATH"); envDir != "" {
		backupDir = envDir
	}

	var backups []StoredBackup

	files, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []StoredBackup{}, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Check if it's a backup file
		name := file.Name()
		if !strings.HasSuffix(name, ".sql") &&
			!strings.HasSuffix(name, ".sql.gz") &&
			!strings.HasSuffix(name, ".sql.gz.enc") {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(backupDir, name)
		backup := StoredBackup{
			Path:        fmt.Sprintf("local://%s", fullPath),
			Size:        info.Size(),
			CreatedAt:   info.ModTime(),
			StorageType: "local",
			Compressed:  strings.Contains(name, ".gz"),
			Encrypted:   strings.Contains(name, ".enc"),
		}

		// Extract ID from filename if possible
		if parts := strings.Split(name, "_"); len(parts) >= 3 {
			backup.ID = strings.Join(parts[:3], "_")
		}

		backups = append(backups, backup)
	}

	return backups, nil
}

func (l *LocalStorage) Delete(remotePath string) error {
	actualPath := strings.TrimPrefix(remotePath, "local://")
	return os.Remove(actualPath)
}

func (l *LocalStorage) GetLocation() string {
	return "local"
}

// AWS S3 Storage Implementation

type S3Storage struct {
	client *s3.S3
	bucket string
	prefix string
	logger *logrus.Logger
}

func NewS3Storage(config S3Config, logger *logrus.Logger) (*S3Storage, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(config.Region),
		Credentials: credentials.NewStaticCredentials(
			config.AccessKey,
			config.SecretKey,
			"",
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &S3Storage{
		client: s3.New(sess),
		bucket: config.Bucket,
		prefix: config.Prefix,
		logger: logger,
	}, nil
}

func (s *S3Storage) Upload(localPath, backupID string) ([]string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	defer file.Close()

	fileName := filepath.Base(localPath)
	key := fmt.Sprintf("%s/%s", s.prefix, fileName)
	if s.prefix == "" {
		key = fileName
	}

	_, err = s.client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   file,
		Metadata: map[string]*string{
			"backup-id": aws.String(backupID),
			"uploaded":  aws.String(time.Now().Format(time.RFC3339)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to S3: %w", err)
	}

	location := fmt.Sprintf("s3://%s/%s", s.bucket, key)
	return []string{location}, nil
}

func (s *S3Storage) Download(remotePath, localPath string) error {
	// Parse S3 path (s3://bucket/key)
	parts := strings.SplitN(strings.TrimPrefix(remotePath, "s3://"), "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid S3 path: %s", remotePath)
	}

	bucket, key := parts[0], parts[1]

	result, err := s.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to download from S3: %w", err)
	}
	defer result.Body.Close()

	outFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, result.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (s *S3Storage) List() ([]StoredBackup, error) {
	var backups []StoredBackup

	err := s.client.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	}, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			backup := StoredBackup{
				Path:        fmt.Sprintf("s3://%s/%s", s.bucket, *obj.Key),
				Size:        *obj.Size,
				CreatedAt:   *obj.LastModified,
				StorageType: "s3",
			}

			// Try to get backup ID from metadata
			if headResult, err := s.client.HeadObject(&s3.HeadObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    obj.Key,
			}); err == nil && headResult.Metadata["backup-id"] != nil {
				backup.ID = *headResult.Metadata["backup-id"]
			}

			backups = append(backups, backup)
		}
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list S3 objects: %w", err)
	}

	return backups, nil
}

func (s *S3Storage) Delete(remotePath string) error {
	// Parse S3 path
	parts := strings.SplitN(strings.TrimPrefix(remotePath, "s3://"), "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid S3 path: %s", remotePath)
	}

	bucket, key := parts[0], parts[1]

	_, err := s.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

func (s *S3Storage) GetLocation() string {
	return fmt.Sprintf("s3://%s", s.bucket)
}

// Google Cloud Storage Implementation

type GCSStorage struct {
	client *storage.Client
	bucket string
	prefix string
	logger *logrus.Logger
}

func NewGCSStorage(config GCSConfig, logger *logrus.Logger) (*GCSStorage, error) {
	var client *storage.Client
	var err error

	if config.ServiceAccountKey != "" {
		client, err = storage.NewClient(context.Background(), option.WithCredentialsFile(config.ServiceAccountKey))
	} else {
		client, err = storage.NewClient(context.Background())
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &GCSStorage{
		client: client,
		bucket: config.Bucket,
		prefix: config.Prefix,
		logger: logger,
	}, nil
}

func (g *GCSStorage) Upload(localPath, backupID string) ([]string, error) {
	ctx := context.Background()
	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	defer file.Close()

	fileName := filepath.Base(localPath)
	objectName := fmt.Sprintf("%s/%s", g.prefix, fileName)
	if g.prefix == "" {
		objectName = fileName
	}

	bucket := g.client.Bucket(g.bucket)
	obj := bucket.Object(objectName)

	writer := obj.NewWriter(ctx)
	writer.Metadata = map[string]string{
		"backup-id": backupID,
		"uploaded":  time.Now().Format(time.RFC3339),
	}

	if _, err := io.Copy(writer, file); err != nil {
		writer.Close()
		return nil, fmt.Errorf("failed to upload to GCS: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close GCS writer: %w", err)
	}

	location := fmt.Sprintf("gcs://%s/%s", g.bucket, objectName)
	return []string{location}, nil
}

func (g *GCSStorage) Download(remotePath, localPath string) error {
	// Parse GCS path (gcs://bucket/object)
	parts := strings.SplitN(strings.TrimPrefix(remotePath, "gcs://"), "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid GCS path: %s", remotePath)
	}

	bucket, objectName := parts[0], parts[1]

	ctx := context.Background()
	reader, err := g.client.Bucket(bucket).Object(objectName).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to create GCS reader: %w", err)
	}
	defer reader.Close()

	outFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, reader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (g *GCSStorage) List() ([]StoredBackup, error) {
	ctx := context.Background()
	var backups []StoredBackup

	bucket := g.client.Bucket(g.bucket)
	query := &storage.Query{Prefix: g.prefix}

	it := bucket.Objects(ctx, query)
	for {
		attrs, err := it.Next()
		if err == storage.ErrObjectNotExist {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate GCS objects: %w", err)
		}

		backup := StoredBackup{
			Path:        fmt.Sprintf("gcs://%s/%s", g.bucket, attrs.Name),
			Size:        attrs.Size,
			CreatedAt:   attrs.Created,
			StorageType: "gcs",
		}

		if attrs.Metadata["backup-id"] != "" {
			backup.ID = attrs.Metadata["backup-id"]
		}

		backups = append(backups, backup)
	}

	return backups, nil
}

func (g *GCSStorage) Delete(remotePath string) error {
	// Parse GCS path
	parts := strings.SplitN(strings.TrimPrefix(remotePath, "gcs://"), "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid GCS path: %s", remotePath)
	}

	bucket, objectName := parts[0], parts[1]

	ctx := context.Background()
	err := g.client.Bucket(bucket).Object(objectName).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete from GCS: %w", err)
	}

	return nil
}

func (g *GCSStorage) GetLocation() string {
	return fmt.Sprintf("gcs://%s", g.bucket)
}

// Azure Blob Storage Implementation

type AzureStorage struct {
	serviceURL azblob.ServiceURL
	container  string
	prefix     string
	logger     *logrus.Logger
}

func NewAzureStorage(config AzureConfig, logger *logrus.Logger) (*AzureStorage, error) {
	credential, err := azblob.NewSharedKeyCredential(config.Account, config.AccessKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credentials: %w", err)
	}

	p := azblob.NewPipeline(credential, azblob.PipelineOptions{})
	url, _ := azblob.NewServiceURL(
		fmt.Sprintf("https://%s.blob.core.windows.net", config.Account),
		p,
	)

	return &AzureStorage{
		serviceURL: url,
		container:  config.Container,
		prefix:     config.Prefix,
		logger:     logger,
	}, nil
}

func (a *AzureStorage) Upload(localPath, backupID string) ([]string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	defer file.Close()

	fileName := filepath.Base(localPath)
	blobName := fmt.Sprintf("%s/%s", a.prefix, fileName)
	if a.prefix == "" {
		blobName = fileName
	}

	ctx := context.Background()
	containerURL := a.serviceURL.NewContainerURL(a.container)
	blobURL := containerURL.NewBlockBlobURL(blobName)

	_, err = azblob.UploadFileToBlockBlob(ctx, file, blobURL, azblob.UploadToBlockBlobOptions{
		BlockSize:   4 * 1024 * 1024,
		Parallelism: 16,
		Metadata: azblob.Metadata{
			"backup-id": backupID,
			"uploaded":  time.Now().Format(time.RFC3339),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to Azure: %w", err)
	}

	location := fmt.Sprintf("azure://%s/%s/%s", a.serviceURL.String(), a.container, blobName)
	return []string{location}, nil
}

func (a *AzureStorage) Download(remotePath, localPath string) error {
	// Parse Azure path and extract blob name
	// This is a simplified implementation
	ctx := context.Background()
	containerURL := a.serviceURL.NewContainerURL(a.container)

	// Extract blob name from remote path
	// Format: azure://account.blob.core.windows.net/container/blobname
	parts := strings.Split(remotePath, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid Azure path: %s", remotePath)
	}
	blobName := strings.Join(parts[len(parts)-1:], "/")

	blobURL := containerURL.NewBlockBlobURL(blobName)

	outFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer outFile.Close()

	err = azblob.DownloadBlobToFile(ctx, blobURL, 0, azblob.CountToEnd, outFile, azblob.DownloadFromBlobOptions{})
	if err != nil {
		return fmt.Errorf("failed to download from Azure: %w", err)
	}

	return nil
}

func (a *AzureStorage) List() ([]StoredBackup, error) {
	ctx := context.Background()
	var backups []StoredBackup

	containerURL := a.serviceURL.NewContainerURL(a.container)

	for marker := (azblob.Marker{}); marker.NotDone(); {
		listBlob, err := containerURL.ListBlobsFlatSegment(ctx, marker, azblob.ListBlobsSegmentOptions{
			Prefix: a.prefix,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list Azure blobs: %w", err)
		}

		marker = listBlob.NextMarker

		for _, blobInfo := range listBlob.Segment.BlobItems {
			backup := StoredBackup{
				Path:        fmt.Sprintf("azure://%s/%s/%s", a.serviceURL.String(), a.container, blobInfo.Name),
				Size:        *blobInfo.Properties.ContentLength,
				CreatedAt:   blobInfo.Properties.CreationTime,
				StorageType: "azure",
			}

			if blobInfo.Metadata["backup-id"] != "" {
				backup.ID = blobInfo.Metadata["backup-id"]
			}

			backups = append(backups, backup)
		}
	}

	return backups, nil
}

func (a *AzureStorage) Delete(remotePath string) error {
	// Extract blob name from remote path
	parts := strings.Split(remotePath, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid Azure path: %s", remotePath)
	}
	blobName := strings.Join(parts[len(parts)-1:], "/")

	ctx := context.Background()
	containerURL := a.serviceURL.NewContainerURL(a.container)
	blobURL := containerURL.NewBlockBlobURL(blobName)

	_, err := blobURL.Delete(ctx, azblob.DeleteSnapshotsOptionInclude, azblob.BlobAccessConditions{})
	if err != nil {
		return fmt.Errorf("failed to delete from Azure: %w", err)
	}

	return nil
}

func (a *AzureStorage) GetLocation() string {
	return fmt.Sprintf("azure://%s", a.container)
}

// Utility function to copy files
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
