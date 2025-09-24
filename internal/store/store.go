// Package store combines WAL and SwissMap.
package store

import (
	"bytes"
	"fmt"
	"sync"

	csmap "github.com/mhmtszr/concurrent-swiss-map"
)

// Store represents a WAL-backed key/value store.
type Store struct {
	wal  *WAL
	data *csmap.CsMap[string, []byte]
	mu   sync.Mutex
}

// New creates a store backed by the provided WAL file path and runs recovery.
func New(walPath string) (*Store, error) {
	wal, err := NewWAL(walPath)
	if err != nil {
		return nil, err
	}

	s := &Store{
		wal:  wal,
		data: csmap.Create[string, []byte](),
	}

	if err := s.Recover(); err != nil {
		_ = wal.Close()
		return nil, err
	}

	return s, nil
}

// Recover replays the WAL to reconstruct in-memory state.
func (s *Store) Recover() error {
	entries, err := s.wal.ReadAll()
	if err != nil {
		return fmt.Errorf("store: recover wal: %w", err)
	}

	for _, entry := range entries {
		s.applyEntry(entry)
	}

	return nil
}

// Get returns a copy of the stored value for the key.
func (s *Store) Get(key string) ([]byte, bool) {
	value, ok := s.data.Load(key)
	if !ok {
		return nil, false
	}

	copyValue := bytes.Clone(value)
	return copyValue, true
}

// Set writes the value for the provided key and persists the mutation to the WAL.
func (s *Store) Set(key string, value []byte) error {
	if key == "" {
		return fmt.Errorf("store: key must not be empty")
	}

	valueCopy := bytes.Clone(value)

	entry := WALEntry{Type: OperationSet, Key: key, Value: valueCopy}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.wal.Append(entry); err != nil {
		return err
	}

	s.data.Store(key, valueCopy)
	return nil
}

// Delete removes the key from the store and records the mutation.
func (s *Store) Delete(key string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("store: key must not be empty")
	}

	entry := WALEntry{Type: OperationDelete, Key: key}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.wal.Append(entry); err != nil {
		return false, err
	}

	existed := s.data.Delete(key)
	return existed, nil
}

// Close finishes pending writes and closes the WAL file.
func (s *Store) Close() error {
	return s.wal.Close()
}

func (s *Store) applyEntry(entry WALEntry) {
	switch entry.Type {
	case OperationSet:
		s.data.Store(entry.Key, entry.Value)
	case OperationDelete:
		s.data.Delete(entry.Key)
	default:
		// Unknown entries are ignored to keep recovery tolerant.
	}
}
