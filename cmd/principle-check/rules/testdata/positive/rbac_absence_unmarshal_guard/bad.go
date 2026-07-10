// Positive-control fixture for rbac_absence_unmarshal_guard.
// getItem -> err check -> json.Unmarshal with NO len(data)==0 guard between.
package badfix

import "encoding/json"

type srvT struct{}

func (srvT) getItem(key string) ([]byte, error) { return nil, nil }

type item struct {
	Name string `json:"name"`
}

func load(srv srvT, key string) (*item, error) {
	it := &item{}
	data, err := srv.getItem(key)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(data, it) // BAD: no len(data)==0 guard; nil bytes -> decode error
	return it, nil
}
