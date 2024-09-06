package kvstore

import "errors"

var (
	FailedToMarshallErr   = errors.New("failed to marshall incoming data")
	FailedToUnmarshallErr = errors.New("failed to unmarshall for requested result")
	NotFoundInStore       = errors.New("not found in the store")
)
