// Copyright 2021 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package appconfig

import (
	"fmt"

	"gopkg.in/yaml.v2"
)

// YAMLRemoteRefParser parses b as a YAML-encoded RemoteRef. It assumes all
// parsing errors mean the content is not a RemoteRef.
func YAMLRemoteRefParser(path string, b []byte) (*RemoteRef, error) {
	var maybeRef struct {
		Remote *string `yaml:"remote"`
		Path   *string `yaml:"path"`
		Ref    *string `yaml:"ref"`
	}

	if err := yaml.UnmarshalStrict(b, &maybeRef); err != nil {
		// assume errors mean this isn't a remote config
		return nil, nil
	}
	if maybeRef.Remote == nil && maybeRef.Path == nil && maybeRef.Ref == nil {
		return nil, nil
	}

	ref := RemoteRef{}
	if err := copyField(&ref.Remote, maybeRef.Remote, "remote"); err != nil {
		return nil, err
	}
	if err := copyField(&ref.Path, maybeRef.Path, "path"); err != nil {
		return nil, err
	}
	if err := copyField(&ref.Ref, maybeRef.Ref, "ref"); err != nil {
		// do nothing, ref is not a required field
	}
	return &ref, nil
}

func copyField(dst, src *string, name string) error {
	if src != nil {
		*dst = *src
	}
	if *dst == "" {
		return fmt.Errorf("invalid remote reference: empty %q field", name)
	}
	return nil
}
