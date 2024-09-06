package kvstore

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func generateStoreName() string {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}

	pid := os.Getpid()
	timestamp := time.Now().Format("20060102-150405")

	return fmt.Sprintf(".kvstore-%s-%d-%s", hostname, pid, timestamp)
}

func generatePersistenceDir(basePath string) string {
	storeName := generateStoreName()

	return filepath.Join(basePath, storeName)
}
