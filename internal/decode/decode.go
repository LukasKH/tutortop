// Package decode turns JSON, YAML and XML into typed values, so a caller
// reads the result and its error on one line instead of passing a pointer to
// fill (STYLEGUIDE.md, "Return values, not out-parameters").
//
// Like the standard decoders, each keeps the fields that decoded when
// another one doesn't, and returns them with the error. The agent relies on
// this: a tool call's file_path must survive an odd value elsewhere in its
// input, or the permission check would see no path.
package decode

import (
	"encoding/json"
	"encoding/xml"

	"gopkg.in/yaml.v3"
)

// JSON returns the T that JSON b holds.
func JSON[T any](b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// YAML returns the T that YAML b holds.
func YAML[T any](b []byte) (T, error) {
	var v T
	err := yaml.Unmarshal(b, &v)
	return v, err
}

// XML returns the T that XML b holds.
func XML[T any](b []byte) (T, error) {
	var v T
	err := xml.Unmarshal(b, &v)
	return v, err
}
