package daemonclient

import (
	"gopkg.in/yaml.v3"
)

// unmarshalYAML is a thin wrapper so client.go does not import yaml directly in
// two places.
func unmarshalYAML(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}
