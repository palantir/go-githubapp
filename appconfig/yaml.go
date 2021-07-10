package appconfig

import (
	"errors"

	"gopkg.in/yaml.v2"
)

// YAMLRemoteRefParser parses b as a YAML-encoded RemoteRef. It assumes all
// parsing errors mean the content is not a RemoteRef.
func YAMLRemoteRefParser(path string, b []byte) (*RemoteRef, error) {
	var ref RemoteRef
	if err := yaml.UnmarshalStrict(b, &ref); err != nil {
		// assume errors mean this isn't a remote config
		return nil, nil
	}

	if ref.Remote == "" {
		return nil, errors.New("invalid remote reference: empty \"remote\" field")
	}
	if ref.Path == "" {
		return nil, errors.New("invalid remote references: empty \"path\" field")
	}
	return &ref, nil
}
