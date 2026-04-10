package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

type BackendType string

const TypeLocal BackendType = "local"

type Reader interface {
	Type() string
	Write(ctx context.Context, key string, reader io.Reader, size int64) error
	Read(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	GetSize(ctx context.Context, key string) (int64, error)
	Close() error
}

type LocalBackend struct {
	rootPath string
}

func New(rootPath string) *LocalBackend {
	return &LocalBackend{
		rootPath: rootPath,
	}
}

func (l *LocalBackend) Type() string {
	return string(TypeLocal)
}

func (l *LocalBackend) Write(ctx context.Context, key string, reader io.Reader, size int64) error {
	fullPath := filepath.Join(l.rootPath, key)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, reader)
	return err
}

func (l *LocalBackend) Read(ctx context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(l.rootPath, key)
	return os.Open(fullPath)
}

func (l *LocalBackend) Delete(ctx context.Context, key string) error {
	fullPath := filepath.Join(l.rootPath, key)
	return os.Remove(fullPath)
}

func (l *LocalBackend) Exists(ctx context.Context, key string) (bool, error) {
	fullPath := filepath.Join(l.rootPath, key)
	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l *LocalBackend) GetSize(ctx context.Context, key string) (int64, error) {
	fullPath := filepath.Join(l.rootPath, key)
	info, err := os.Stat(fullPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (l *LocalBackend) Close() error {
	return nil
}

func (l *LocalBackend) RootPath() string {
	return l.rootPath
}

var _ Reader = (*LocalBackend)(nil)

type Backend = Reader
