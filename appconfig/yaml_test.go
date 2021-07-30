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
	"testing"
)

func TestYAMLRemoteRefParser(t *testing.T) {
	tests := map[string]struct {
		Input  string
		Output *RemoteRef
		Error  bool
	}{
		"complete": {
			Input: "{remote: test/test, path: test.yaml, ref: main}",
			Output: &RemoteRef{
				Remote: "test/test",
				Path:   "test.yaml",
				Ref:    "main",
			},
		},
		"missingRemote": {
			Input:  "{path: test.yaml, ref: main}",
			Output: nil,
		},
		"missingPath": {
			Input: "{remote: test/test, ref: main}",
			Output: &RemoteRef{
				Remote: "test/test",
				Ref:    "main",
			},
		},
		"missingRef": {
			Input: "{remote: test/test, path: test.yaml}",
			Output: &RemoteRef{
				Remote: "test/test",
				Path:   "test.yaml",
			},
		},
		"emptyRemote": {
			Input: "{remote: ''}",
			Error: true,
		},
		"empty": {
			Input:  "",
			Output: nil,
		},
		"commentsOnly": {
			Input:  "# the existence of this file enables the app\n",
			Output: nil,
		},
		"extraFields": {
			Input:  "{remote: test/test, path: test.yaml, ref: main, key: value}",
			Output: nil,
		},
		"onlyUnknownFields": {
			Input:  "{key: value}",
			Output: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ref, err := YAMLRemoteRefParser("test.yml", []byte(test.Input))
			if test.Error {
				if err == nil {
					t.Fatal("expected error parsing ref, but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error parsing ref: %v", err)
			}

			switch {
			case test.Output == nil && ref != nil:
				t.Errorf("expected nil ref, but got: %+v", *ref)
			case test.Output != nil && ref == nil:
				t.Errorf("expected %+v, but got nil", *test.Output)
			case test.Output != nil && ref != nil:
				if *test.Output != *ref {
					t.Errorf("expected %+v, but got %+v", *test.Output, *ref)
				}
			}
		})
	}
}
