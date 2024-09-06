package kvstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Persistence struct {
	directory string
	mu        sync.Mutex
}

func newPersistence(dir string) (*Persistence, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	return &Persistence{directory: dir}, nil
}

func (p *Persistence) save(kvs *KvStore) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := kvs.marshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal KvStore: %w", err)
	}

	filename := filepath.Join(p.directory, "kvstore.bin")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (p *Persistence) load(kvs *KvStore) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	filename := filepath.Join(p.directory, "kvstore.bin")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// If the file doesn't exist, it's not an error - just return an empty store
			return nil
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	if err := kvs.unmarshalBinary(data); err != nil {
		return fmt.Errorf("failed to unmarshal KvStore: %w", err)
	}

	return nil
}
