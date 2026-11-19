package ipc

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzReadMessage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		var message Request
		_ = ReadMessage(bytes.NewReader(data), &message)
	})
}

func FuzzRequestJSON(f *testing.F) {
	f.Add([]byte("{}"))
	f.Add([]byte(`{"version":1,"request_id":"r","operation":"ping"}`))
	f.Add([]byte(`{"version":1,"operation_id":"op","payload":null}`))
	f.Add([]byte(`{"version":1,"version":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var request Request
		_ = json.Unmarshal(data, &request)
	})
}
