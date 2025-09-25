// Package store provides Write-Ahead Log functionality
package store

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TODO: Add log rotation and compaction
// TODO: is append ok?

type OperationType string

const (
	OperationSet    OperationType = "set"
	OperationDelete OperationType = "delete"
)

var ErrCorruptWAL = errors.New("store: wal file is corrupted")

type WALEntry struct {
	Type  OperationType
	Key   string
	Value []byte
}

const (
	walFileMode  = 0o644
	lengthPrefix = 4
	checksumSize = 4
	bufferSize   = 100
)

// WAL entry format: [4-byte length][4-byte checksum][payload]
// The checksum is CRC32 of the payload data

type WAL struct {
	mu     sync.Mutex
	path   string
	file   *os.File
	writer *bufio.Writer

	flushChan chan struct{}
	doneChan  chan struct{}

	activeBuffer  []WALEntry
	pendingBuffer []WALEntry
	flushMu       sync.Mutex

	wg     sync.WaitGroup
	ticker *time.Ticker
}

func NewWAL(path string) (*WAL, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("store: create wal directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, walFileMode)
	if err != nil {
		return nil, fmt.Errorf("store: open wal: %w", err)
	}

	wal := &WAL{
		path:   path,
		file:   file,
		writer: bufio.NewWriter(file),

		flushChan: make(chan struct{}, 1),
		doneChan:  make(chan struct{}),

		activeBuffer:  make([]WALEntry, 0, bufferSize),
		pendingBuffer: make([]WALEntry, 0, bufferSize),
	}

	wal.wg.Add(1)
	wal.ticker = time.NewTicker(1 * time.Second)
	go func() {
		defer wal.wg.Done()
		wal.asyncFlush(wal.ticker)
	}()

	return wal, nil
}

func (w *WAL) Append(entry WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.activeBuffer = append(w.activeBuffer, entry)
	if len(w.activeBuffer) >= bufferSize {
		w.flushChan <- struct{}{}
	}

	return nil
}

func (w *WAL) ReadAll() ([]WALEntry, error) {
	w.flushBuffer()
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("store: seek wal start: %w", err)
	}

	reader := bufio.NewReader(w.file)
	entries := make([]WALEntry, 0)
	lengthBuf := make([]byte, lengthPrefix)
	checksumBuf := make([]byte, checksumSize)

	for {
		// Read length prefix
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

		// Read checksum
		if _, err := io.ReadFull(reader, checksumBuf); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, ErrCorruptWAL
			}
			return nil, fmt.Errorf("store: read wal checksum: %w", err)
		}

		expectedChecksum := binary.BigEndian.Uint32(checksumBuf)

		// Read payload
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, ErrCorruptWAL
			}
			return nil, fmt.Errorf("store: read wal payload: %w", err)
		}

		// Validate checksum
		actualChecksum := crc32.ChecksumIEEE(payload)
		if actualChecksum != expectedChecksum {
			return nil, fmt.Errorf("store: checksum validation failed for entry (expected: %d, actual: %d): %w", expectedChecksum, actualChecksum, ErrCorruptWAL)
		}

		// Decode entry
		var entry WALEntry
		buf := bytes.NewReader(payload)
		dec := gob.NewDecoder(buf)
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("store: decode wal entry: %w", err)
		}

		entries = append(entries, entry)
	}

	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("store: seek wal end: %w", err)
	}

	return entries, nil
}

func (w *WAL) Close() error {
	w.ticker.Stop()
	close(w.doneChan)
	w.wg.Wait()
	w.flushBuffer()
	return w.file.Close()
}

func (w *WAL) asyncFlush(t *time.Ticker) {
	for {
		select {
		case <-t.C:
			w.flushBuffer()
		case <-w.flushChan:
			w.flushBuffer()
		case <-w.doneChan:
			w.flushBuffer()
			return
		}
	}
}

func (w *WAL) swapBuffers() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.activeBuffer) == 0 {
		return
	}

	w.activeBuffer, w.pendingBuffer = w.pendingBuffer, w.activeBuffer
}

func (w *WAL) flushBuffer() {
	w.swapBuffers()

	w.flushMu.Lock()
	defer w.flushMu.Unlock()

	for _, entry := range w.pendingBuffer {
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(entry); err != nil {
			continue
		}
		data := buf.Bytes()

		// Calculate CRC32 checksum of the payload
		checksum := crc32.ChecksumIEEE(data)

		// Write length prefix
		var lengthBuf [lengthPrefix]byte
		binary.BigEndian.PutUint32(lengthBuf[:], uint32(len(data)))
		w.writer.Write(lengthBuf[:])

		// Write checksum
		var checksumBuf [checksumSize]byte
		binary.BigEndian.PutUint32(checksumBuf[:], checksum)
		w.writer.Write(checksumBuf[:])

		// Write payload
		w.writer.Write(data)
	}

	w.writer.Flush()
	w.file.Sync()

	w.mu.Lock()
	w.pendingBuffer = w.pendingBuffer[:0]
	w.mu.Unlock()
}
