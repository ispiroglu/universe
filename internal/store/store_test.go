package store

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
)

func TestWALAppendAndReadAll(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	wal, err := NewWAL(walPath)
	if err != nil {
		t.Fatalf("failed to create wal: %v", err)
	}
	t.Cleanup(func() {
		_ = wal.Close()
	})

	entries := []WALEntry{
		{Type: OperationSet, Key: "alpha", Value: []byte("value-1")},
		{Type: OperationDelete, Key: "alpha"},
		{Type: OperationSet, Key: "beta", Value: []byte("value-2")},
	}

	for _, entry := range entries {
		if err := wal.Append(entry); err != nil {
			t.Fatalf("append wal entry: %v", err)
		}
	}

	readEntries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("read wal entries: %v", err)
	}

	if len(readEntries) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(readEntries))
	}

	for i := range entries {
		if entries[i].Type != readEntries[i].Type {
			t.Fatalf("entry %d type mismatch: expected %s got %s", i, entries[i].Type, readEntries[i].Type)
		}
		if entries[i].Key != readEntries[i].Key {
			t.Fatalf("entry %d key mismatch: expected %s got %s", i, entries[i].Key, readEntries[i].Key)
		}
		if !bytes.Equal(entries[i].Value, readEntries[i].Value) {
			t.Fatalf("entry %d value mismatch: expected %q got %q", i, entries[i].Value, readEntries[i].Value)
		}
	}
}

func TestStoreSetGetDelete(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "store.wal")

	store, err := New(walPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := store.Set("foo", []byte("bar")); err != nil {
		t.Fatalf("set value: %v", err)
	}

	got, ok := store.Get("foo")
	if !ok {
		t.Fatalf("expected key to exist")
	}
	if !bytes.Equal(got, []byte("bar")) {
		t.Fatalf("unexpected value: %q", got)
	}

	// Ensure Get returns a copy
	got[0] = 'z'
	again, ok := store.Get("foo")
	if !ok {
		t.Fatalf("key disappeared")
	}
	if !bytes.Equal(again, []byte("bar")) {
		t.Fatalf("value mutated: %q", again)
	}

	deleted, err := store.Delete("foo")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !deleted {
		t.Fatalf("expected delete to report existing key")
	}

	if _, ok := store.Get("foo"); ok {
		t.Fatalf("expected key to be deleted")
	}

	deleted, err = store.Delete("foo")
	if err != nil {
		t.Fatalf("delete missing key: %v", err)
	}
	if deleted {
		t.Fatalf("expected delete to report missing key")
	}
}

func TestStoreRecovery(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "recovery.wal")

	store, err := New(walPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Set("a", []byte("1")); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := store.Set("b", []byte("2")); err != nil {
		t.Fatalf("set b: %v", err)
	}
	if _, err := store.Delete("a"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	store, err = New(walPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	if _, ok := store.Get("a"); ok {
		t.Fatalf("expected key 'a' to be deleted after recovery")
	}

	bVal, ok := store.Get("b")
	if !ok {
		t.Fatalf("expected key 'b' after recovery")
	}
	if !bytes.Equal(bVal, []byte("2")) {
		t.Fatalf("unexpected value for 'b': %q", bVal)
	}
}

func BenchmarkStoreSet(b *testing.B) {
	dir := b.TempDir()
	walPath := filepath.Join(dir, "bench.wal")

	store, err := New(walPath)
	if err != nil {
		b.Fatalf("create store: %v", err)
	}
	defer store.Close()

	value := []byte("benchmark value data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		if err := store.Set(key, value); err != nil {
			b.Fatalf("set: %v", err)
		}
	}
}

func BenchmarkStoreGet(b *testing.B) {
	dir := b.TempDir()
	walPath := filepath.Join(dir, "bench.wal")

	store, err := New(walPath)
	if err != nil {
		b.Fatalf("create store: %v", err)
	}
	defer store.Close()

	value := []byte("benchmark value data")
	key := "test-key"
	if err := store.Set(key, value); err != nil {
		b.Fatalf("set: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := store.Get(key); !ok {
			b.Fatalf("get failed")
		}
	}
}
