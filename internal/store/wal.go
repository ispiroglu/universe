// Package store provides Write-Ahead Log functionality
package store

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// OperationType represents the type of mutation recorded in the WAL.
type OperationType string

const (
	// OperationSet indicates a key/value pair should be stored.
	OperationSet OperationType = "set"
	// OperationDelete indicates a key should be removed.
	OperationDelete OperationType = "delete"
)

// ErrCorruptWAL is returned when the WAL file cannot be parsed correctly.
var ErrCorruptWAL = errors.New("store: wal file is corrupted")

// WALEntry captures a single mutation stored in the write-ahead log.
type WALEntry struct {
	Type  OperationType `json:"type"`
	Key   string        `json:"key"`
	Value []byte        `json:"value,omitempty"`
}

const (
	walFileMode  = 0o644
	lengthPrefix = 4
)

// WAL represents the Write-Ahead Log.
type WAL struct {
	mu     sync.Mutex
	path   string
	file   *os.File
	writer *bufio.Writer
}

// NewWAL opens or creates a WAL file at the provided path.
func NewWAL(path string) (*WAL, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("store: create wal directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, walFileMode)
	if err != nil {
		return nil, fmt.Errorf("store: open wal: %w", err)
	}

	return &WAL{
		path:   path,
		file:   file,
		writer: bufio.NewWriter(file),
	}, nil
}

// Append persists an entry to the WAL.
func (w *WAL) Append(entry WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("store: marshal wal entry: %w", err)
	}

	var prefix [lengthPrefix]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(data)))

	if _, err = w.writer.Write(prefix[:]); err != nil {
		return fmt.Errorf("store: write wal length: %w", err)
	}
	if _, err = w.writer.Write(data); err != nil {
		return fmt.Errorf("store: write wal payload: %w", err)
	}
	if err = w.writer.Flush(); err != nil {
		return fmt.Errorf("store: flush wal: %w", err)
	}
	if err = w.file.Sync(); err != nil {
		return fmt.Errorf("store: sync wal: %w", err)
	}

	return nil
}

// ReadAll replays all entries stored in the WAL.
func (w *WAL) ReadAll() ([]WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return nil, fmt.Errorf("store: flush wal before read: %w", err)
	}

	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("store: seek wal start: %w", err)
	}

	reader := bufio.NewReader(w.file)
	entries := make([]WALEntry, 0)
	lengthBuf := make([]byte, lengthPrefix)

	for {
		if _, err := io.ReadFull(reader, lengthBuf); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, ErrCorruptWAL
			}
			return nil, fmt.Errorf("store: read wal length: %w", err)
		}

		length := binary.BigEndian.Uint32(lengthBuf)
		if length == 0 {
			return nil, ErrCorruptWAL
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, ErrCorruptWAL
			}
			return nil, fmt.Errorf("store: read wal payload: %w", err)
		}

		var entry WALEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			return nil, fmt.Errorf("store: unmarshal wal entry: %w", err)
		}

		entries = append(entries, entry)
	}

	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("store: seek wal end: %w", err)
	}

	return entries, nil
}

// Close releases the underlying file descriptor.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("store: flush wal on close: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("store: sync wal on close: %w", err)
	}
	return w.file.Close()
}
