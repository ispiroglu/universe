package kvstore

import (
	"reflect"
	"testing"
)

// TODO: fix tests
func TestKvStore(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     interface{}
		want      interface{}
		wantErr   bool
		errMsg    string
		getErr    bool
		getErrMsg string
	}{
		{
			name:  "Set and Get string",
			key:   "testKey",
			value: "testValue",
			want:  "testValue",
		},
		{
			name:  "Set and Get int",
			key:   "intKey",
			value: 42,
			want:  42,
		},
		{
			name:  "Set and Get struct",
			key:   "structKey",
			value: struct{ Name string }{"Test"},
			want:  struct{ Name string }{"Test"},
		},
		{
			name:      "Get non-existent key",
			key:       "nonExistentKey",
			getErr:    true,
			getErrMsg: "not found in store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kvs := CreateStore()

			err := kvs.Set(tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.errMsg {
				t.Errorf("Set() error message = %v, want %v", err.Error(), tt.errMsg)
				return
			}

			var got interface{}
			err = kvs.Get(tt.key, &got)
			if (err != nil) != tt.getErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.getErr)
				return
			}
			if tt.getErr && err.Error() != tt.getErrMsg {
				t.Errorf("Get() error message = %v, want %v", err.Error(), tt.getErrMsg)
				return
			}

			if !tt.getErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Get() got = %v, want %v", got, tt.want)
			}

			kvs.Delete(tt.key)
			err = kvs.Get(tt.key, &got)
			if err == nil {
				t.Errorf("Delete() failed, key still exists")
			}
		})
	}
}

func TestKvStorePersistence(t *testing.T) {
	kvs := CreateStore()

	// Set some values
	kvs.Set("key1", "value1")
	kvs.Set("key2", 42)

	// Save to disk
	err := kvs.SaveToDisk()
	if err != nil {
		t.Fatalf("SaveToDisk() error = %v", err)
	}

	// Create a new store and load from disk
	newKvs := CreateStore()
	err = newKvs.LoadFromDisk()
	if err != nil {
		t.Fatalf("LoadFromDisk() error = %v", err)
	}

	// Check if values are correctly loaded
	tests := []struct {
		key      string
		expected interface{}
	}{
		{"key1", "value1"},
		{"key2", 42},
	}

	for _, tt := range tests {
		var got interface{}
		err := newKvs.Get(tt.key, &got)
		if err != nil {
			t.Errorf("Get(%v) error = %v", tt.key, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("Get(%v) = %v, want %v", tt.key, got, tt.expected)
		}
	}
}
