// @title Universe API
// @version 1.0
// @description A distributed key-value store API
// @host localhost:8080
// @BasePath /
package main

import (
	"fmt"
	"universe/internal/server/http"
	"universe/internal/store"
)

func main() {
	fmt.Println("Universe KV Server starting...")

	store, err := store.New("universe.wal")
	if err != nil {
		panic(err)
	}
	defer store.Close()

	httpServer := http.NewServer(store)
	if err := httpServer.Start(); err != nil {
		panic(err)
	}

	defer httpServer.Stop()
}
