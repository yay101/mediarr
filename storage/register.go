package storage

import (
	"github.com/yay101/mediarr/db"
	"github.com/yay101/mediarr/storage/local"
	"github.com/yay101/mediarr/storage/s3"
)

func init() {
	// Register local backend
	Register("local", func(loc *db.StorageLocation) (StorageBackend, error) {
		return local.New(loc.Path), nil
	})

	// Register S3 backend
	Register("s3", func(loc *db.StorageLocation) (StorageBackend, error) {
		return s3.New(s3.Options{
			Bucket:         loc.Bucket,
			Region:         loc.Region,
			Endpoint:       loc.Endpoint,
			AccessKey:      loc.AccessKey,
			SecretKey:      loc.SecretKey,
			ForcePathStyle: loc.ForcePathStyle,
		})
	})
}
