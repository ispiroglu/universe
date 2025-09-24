# Makefile for Universe project

.PHONY: build test clean

build:
	go build ./cmd/...

test:
	go test ./...

clean:
	go clean ./...