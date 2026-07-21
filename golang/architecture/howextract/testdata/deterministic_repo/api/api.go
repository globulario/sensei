package api

type Store interface { Get(string) string }

func Read(s Store, key string) string { return s.Get(key) }
