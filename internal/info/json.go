package info

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var ErrJSONUnavailable = errors.New("JSON data unavailable")

// loadJSONFile reads and validates one Flask data file without transforming
// its JSON shape or array order.
func loadJSONFile(path string) (json.RawMessage, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJSONUnavailable, err)
	}
	contents = bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})
	if !json.Valid(contents) {
		return nil, ErrJSONUnavailable
	}
	return append(json.RawMessage(nil), contents...), nil
}
