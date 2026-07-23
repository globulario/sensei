package api

type Store interface { Get(string) string }

func Read(s Store, key string) string { return s.Get(key) }

type Request struct {
	UserID string `json:"user_id"`
}

type Response struct {
	Value string `json:"value"`
}

type Service interface {
	Get(Request) Response
}
