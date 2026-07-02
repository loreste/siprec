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
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
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

// StorageConfig defines backup storage options
type StorageConfig struct {
	Local bool
	S3    S3Config
	GCS   GCSConfig
	Azure AzureConfig
}

// S3Config for AWS S3 storage
type S3Config struct {
	Enabled   bool
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string
}

// GCSConfig for Google Cloud Storage
type GCSConfig struct {
	Enabled           bool
	Bucket            string
	ServiceAccountKey string
	Prefix            string
}

// AzureConfig for Azure Blob Storage.
//
// Authentication precedence (first match wins):
//  1. SASToken  – shared access signature scoped to a container (recommended, least privilege)
//  2. AccessKey – storage account key (full account access; discouraged)
type AzureConfig struct {
	Enabled   bool
	Account   string
	Container string
	SASToken  string
	AccessKey string
	Prefix    string
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

// Delete deletes from all storage backends matching the remote path scheme
func (m *MultiBackupStorage) Delete(remotePath string) error {
	var errors []string
	scheme := extractScheme(remotePath)
	matched := false

	for _, storage := range m.storages {
		if !storageMatchesScheme(storage.GetLocation(), scheme) {
			continue
		}
		matched = true
		err := storage.Delete(remotePath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", storage.GetLocation(), err))
		}
	}

	if !matched {
		return fmt.Errorf("no storage backend configured for scheme %s", scheme)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to delete from some storage backends: %s", strings.Join(errors, "; "))
	}

	return nil
}

func extractScheme(path string) string {
	if strings.HasPrefix(path, "local://") {
		return "local"
	}
	if idx := strings.Index(path, "://"); idx != -1 {
		return path[:idx]
	}
	return ""
}

func storageMatchesScheme(location, scheme string) bool {
	if scheme == "" {
		return false
	}
	// Exact match
	if location == scheme {
		return true
	}
	// Match URL format: location must start with "scheme://"
	schemePrefix := scheme + "://"
	if strings.HasPrefix(location, schemePrefix) {
		return true
	}
	// Prefix match, but not for ambiguous cases like "gcs" vs "gs"
	// Only match if location starts with scheme followed by non-letter
	if strings.HasPrefix(location, scheme) {
		// Check that the character after the scheme is not a letter
		// This prevents "gcs" from matching "gs"
		if len(location) > len(scheme) {
			nextChar := location[len(scheme)]
			// Allow dash, underscore, or other non-letter characters
			if nextChar < 'a' || nextChar > 'z' {
				return true
			}
		}
	}
	return false
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
			"backup_id": aws.String(backupID),
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
			}); err == nil && headResult.Metadata["backup_id"] != nil {
				backup.ID = *headResult.Metadata["backup_id"]
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
		"backup_id": backupID,
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

		if attrs.Metadata["backup_id"] != "" {
			backup.ID = attrs.Metadata["backup_id"]
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
	client    *azblob.Client
	account   string
	container string
	prefix    string
	logger    *logrus.Logger
}

// NewAzureStorage builds an Azure Blob client using the modern
// github.com/Azure/azure-sdk-for-go/sdk/storage/azblob SDK.
//
// Auth precedence (first match wins): SAS token, then account key. At least one
// must be configured. Using the account key logs a warning because it grants
// full access to the whole storage account; a container-scoped SAS token is
// preferred (least privilege).
func NewAzureStorage(config AzureConfig, logger *logrus.Logger) (*AzureStorage, error) {
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", config.Account)

	var client *azblob.Client
	var err error

	switch {
	case config.SASToken != "":
		// SAS token is appended to the service URL. Accept it with or without a
		// leading '?' so operators can paste either form.
		sas := strings.TrimPrefix(config.SASToken, "?")
		client, err = azblob.NewClientWithNoCredential(serviceURL+"?"+sas, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client with SAS token: %w", err)
		}
	case config.AccessKey != "":
		if logger != nil {
			logger.Warn("Azure Blob Storage nutzt Account Key Auth – SAS-Token wird empfohlen (least privilege).")
		}
		cred, credErr := azblob.NewSharedKeyCredential(config.Account, config.AccessKey)
		if credErr != nil {
			return nil, fmt.Errorf("failed to create Azure shared key credential: %w", credErr)
		}
		client, err = azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client with shared key: %w", err)
		}
	default:
		return nil, fmt.Errorf("azure storage requires an auth method: set SASToken or AccessKey")
	}

	return &AzureStorage{
		client:    client,
		account:   config.Account,
		container: config.Container,
		prefix:    config.Prefix,
		logger:    logger,
	}, nil
}

// blobLocation builds a credential-free azure:// location string from stored
// values. It intentionally never derives from client.URL(), which would embed
// the SAS token when SAS auth is used.
func (a *AzureStorage) blobLocation(blobName string) string {
	return fmt.Sprintf("azure://%s.blob.core.windows.net/%s/%s", a.account, a.container, blobName)
}

// blobNameFromLocation extracts the blob name (including any prefix path) from a
// remote location produced by blobLocation.
func (a *AzureStorage) blobNameFromLocation(remotePath string) string {
	marker := fmt.Sprintf("/%s/", a.container)
	if idx := strings.Index(remotePath, marker); idx >= 0 {
		return remotePath[idx+len(marker):]
	}
	// Fall back to the trailing path segment for legacy/unknown formats.
	parts := strings.Split(remotePath, "/")
	return parts[len(parts)-1]
}

func (a *AzureStorage) Upload(localPath, backupID string) ([]string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	defer file.Close()

	fileName := filepath.Base(localPath)
	blobName := fileName
	if a.prefix != "" {
		blobName = fmt.Sprintf("%s/%s", a.prefix, fileName)
	}

	uploaded := time.Now().Format(time.RFC3339)
	ctx := context.Background()
	_, err = a.client.UploadFile(ctx, a.container, blobName, file, &azblob.UploadFileOptions{
		BlockSize: 4 * 1024 * 1024,
		// Metadata keys must not contain hyphens; Azure rejects them with HTTP 400.
		// Keep "backup_id" (underscore). See docs/upstream-patches.md Fix 1.
		Metadata: map[string]*string{
			"backup_id": &backupID,
			"uploaded":  &uploaded,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to Azure: %w", err)
	}

	return []string{a.blobLocation(blobName)}, nil
}

func (a *AzureStorage) Download(remotePath, localPath string) error {
	blobName := a.blobNameFromLocation(remotePath)
	if blobName == "" {
		return fmt.Errorf("invalid Azure path: %s", remotePath)
	}

	outFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer outFile.Close()

	ctx := context.Background()
	if _, err := a.client.DownloadFile(ctx, a.container, blobName, outFile, nil); err != nil {
		return fmt.Errorf("failed to download from Azure: %w", err)
	}

	return nil
}

func (a *AzureStorage) List() ([]StoredBackup, error) {
	ctx := context.Background()
	var backups []StoredBackup

	var prefix *string
	if a.prefix != "" {
		prefix = &a.prefix
	}

	pager := a.client.NewListBlobsFlatPager(a.container, &azblob.ListBlobsFlatOptions{
		Prefix:  prefix,
		Include: azblob.ListBlobsInclude{Metadata: true},
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Azure blobs: %w", err)
		}
		if page.Segment == nil {
			continue
		}

		for _, blobInfo := range page.Segment.BlobItems {
			if blobInfo == nil || blobInfo.Name == nil {
				continue
			}

			backup := StoredBackup{
				Path:        a.blobLocation(*blobInfo.Name),
				StorageType: "azure",
			}

			if props := blobInfo.Properties; props != nil {
				if props.ContentLength != nil {
					backup.Size = *props.ContentLength
				}
				if props.CreationTime != nil {
					backup.CreatedAt = *props.CreationTime
				}
			}

			if id := blobInfo.Metadata["backup_id"]; id != nil && *id != "" {
				backup.ID = *id
			}

			backups = append(backups, backup)
		}
	}

	return backups, nil
}

func (a *AzureStorage) Delete(remotePath string) error {
	blobName := a.blobNameFromLocation(remotePath)
	if blobName == "" {
		return fmt.Errorf("invalid Azure path: %s", remotePath)
	}

	ctx := context.Background()
	if _, err := a.client.DeleteBlob(ctx, a.container, blobName, nil); err != nil {
		return fmt.Errorf("failed to delete from Azure: %w", err)
	}

	return nil
}

func (a *AzureStorage) GetLocation() string {
	return fmt.Sprintf("azure://%s.blob.core.windows.net/%s", a.account, a.container)
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
