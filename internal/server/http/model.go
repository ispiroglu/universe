package http

type SetRequest struct {
	Key   string `path:"key"`
	Value []byte `json:"value"`
}

type SetBody struct {
	Value any `json:"value"`
}

type GetRequest struct {
	Key string `path:"key"`
}

type DeleteRequest struct {
	Key string `path:"key"`
}
