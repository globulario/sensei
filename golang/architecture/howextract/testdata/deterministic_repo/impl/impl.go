package impl

import "example.com/deterministic/api"

type Memory struct{ values map[string]string }

func New() *Memory { return &Memory{values: map[string]string{"key": "value"}} }
func (m *Memory) Get(key string) string { return m.values[key] }
func Use() string { return api.Read(New(), "key") }

type ServiceImpl struct{}

func (ServiceImpl) Get(req api.Request) api.Response {
	return api.Response{Value: "ok"}
}

type Client struct {
	svc api.Service
}

func (c *Client) Call(req api.Request) api.Response {
	return c.svc.Get(req)
}

type privateStruct struct {
	field string
}
