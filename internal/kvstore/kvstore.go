package kvstore

import (
	"bytes"
	"encoding/gob"

	"github.com/bytedance/sonic"
	csmap "github.com/mhmtszr/concurrent-swiss-map"
)

// TODO: gracefull shutdown
// TODO: Write ahead logging
type KvStore struct {
	sm *csmap.CsMap[string, []byte]
	p  *Persistence
}

func CreateStore() *KvStore {
	sm := csmap.Create[string, []byte](
		csmap.WithShardCount[string, []byte](32),
		csmap.WithSize[string, []byte](1000),
	)

	name := generateStoreName()
	persistenceDir := generatePersistenceDir(name)
	persistence, err := newPersistence(persistenceDir)
	if err != nil {
		panic(err)
	}

	kvs := &KvStore{
		sm: sm,
		p:  persistence,
	}

	if err := persistence.load(kvs); err != nil {
		panic(err)
	}

	return kvs
}

func (kvs *KvStore) Set(k string, v any) error {
	byteArr, err := sonic.Marshal(v)
	if err != nil {
		return FailedToMarshallErr
	}

	kvs.sm.Store(k, byteArr)
	return nil
}

func (kvs *KvStore) Get(k string, res any) error {
	byteArr, found := kvs.sm.Load(k)
	if !found {
		return NotFoundInStore
	}

	if err := sonic.Unmarshal(byteArr, res); err != nil {
		return FailedToUnmarshallErr
	}

	return nil
}

func (kvs *KvStore) Delete(k string) {
	kvs.sm.Delete(k)
}

func (kvs *KvStore) SaveToDisk() error {
	return kvs.p.save(kvs)
}

func (kvs *KvStore) LoadFromDisk() error {
	return kvs.p.load(kvs)
}

// TODO: Faster(bulk) marshall?
func (kvs *KvStore) marshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	tmp := make(map[string][]byte, kvs.sm.Count())
	kvs.sm.Range(func(key string, value []byte) (stop bool) {
		tmp[key] = value
		return true
	})

	if err := enc.Encode(tmp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TODO: Faster(bulk) unmarshall?
func (kvs *KvStore) unmarshalBinary(data []byte) error {
	dec := gob.NewDecoder(bytes.NewReader(data))

	tmp := make(map[string][]byte)
	if err := dec.Decode(&tmp); err != nil {
		return err
	}

	for key, val := range tmp {
		kvs.Set(key, val)
	}

	return nil
}
