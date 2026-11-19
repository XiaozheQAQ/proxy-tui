package ipc

import "encoding/json"

func marshal(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func unmarshal(data json.RawMessage, out any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
