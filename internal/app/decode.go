package app

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// decodeJSON returns the T that JSON b holds. Like json.Unmarshal, it keeps
// the fields that decoded when another one doesn't.
func decodeJSON[T any](b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// decodeYAML returns the T that YAML b holds. Like yaml.Unmarshal, it keeps
// the fields that decoded when another one doesn't.
func decodeYAML[T any](b []byte) (T, error) {
	var v T
	err := yaml.Unmarshal(b, &v)
	return v, err
}
