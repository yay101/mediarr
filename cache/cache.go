package cache

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	Location = "/tmp/mediarr-cache"
	mu       sync.RWMutex
	cacheDir string
)

func init() {
	cacheDir = Location
}

type Cache struct {
	Identifier string
	Expire     bool
	Expiry     time.Time
}

type cacheFile struct {
	Header Cache
	Data   []byte
}

func Get[T any](id string) (data []T, ok bool) {
	mu.RLock()
	defer mu.RUnlock()

	path := filepath.Join(cacheDir, id+".cache")
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	var cf cacheFile
	if err := gob.NewDecoder(f).Decode(&cf); err != nil {
		os.Remove(path)
		return nil, false
	}

	if cf.Header.Expire && time.Now().After(cf.Header.Expiry) {
		os.Remove(path)
		return nil, false
	}

	var result []T
	if err := gob.NewDecoder(bytes.NewReader(cf.Data)).Decode(&result); err != nil {
		return nil, false
	}

	return result, true
}

func Set[T any](id string, data []T, expiry time.Duration) bool {
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return false
	}

	header := Cache{
		Identifier: id,
		Expire:     expiry > 0,
		Expiry:     time.Now().Add(expiry),
	}

	var dataBuf bytes.Buffer
	if err := gob.NewEncoder(&dataBuf).Encode(data); err != nil {
		return false
	}

	cf := cacheFile{
		Header: header,
		Data:   dataBuf.Bytes(),
	}

	path := filepath.Join(cacheDir, id+".cache")
	f, err := os.Create(path)
	if err != nil {
		return false
	}
	defer f.Close()

	if err := gob.NewEncoder(f).Encode(&cf); err != nil {
		return false
	}

	return true
}
