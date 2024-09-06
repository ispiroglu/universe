package kvstore

import (
	"github.com/bytedance/sonic"
	csmap "github.com/mhmtszr/concurrent-swiss-map"
)

type KvStore struct {
	sm *csmap.CsMap[string, []byte]
}

func CreateStore() *KvStore {
	sm := csmap.Create(
		csmap.WithShardCount[string, int](32),
		csmap.WithSize[string, int](1000),
	)

	return &KvStore{
		sm: sm,
	}
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
