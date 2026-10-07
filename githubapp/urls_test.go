// Copyright 2024 Palantir Technologies, Inc.
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

package githubapp

import (
	"testing"
)

func TestParseURLs(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantWeb string
		wantV3  string
		wantV4  string
		wantErr bool
	}{
		"publicDomain": {
			input:   "github.com",
			wantWeb: "https://github.com",
			wantV3:  "https://api.github.com/",
			wantV4:  "https://api.github.com/graphql",
		},
		"publicHTTPS": {
			input:   "https://github.com",
			wantWeb: "https://github.com",
			wantV3:  "https://api.github.com/",
			wantV4:  "https://api.github.com/graphql",
		},
		"enterprise": {
			input:   "https://github.example.com",
			wantWeb: "https://github.example.com/",
			wantV3:  "https://github.example.com/api/v3/",
			wantV4:  "https://github.example.com/api/graphql",
		},
		"enterpriseNoScheme": {
			input:   "github.example.com",
			wantWeb: "https://github.example.com/",
			wantV3:  "https://github.example.com/api/v3/",
			wantV4:  "https://github.example.com/api/graphql",
		},
		"enterpriseWithPath": {
			input:   "https://github.example.com/some/path",
			wantWeb: "https://github.example.com/",
			wantV3:  "https://github.example.com/api/v3/",
			wantV4:  "https://github.example.com/api/graphql",
		},
		"empty": {
			input:   "",
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseURLs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Web.String() != tt.wantWeb {
				t.Errorf("Web: got %q, want %q", got.Web, tt.wantWeb)
			}
			if got.APIv3.String() != tt.wantV3 {
				t.Errorf("APIv3: got %q, want %q", got.APIv3, tt.wantV3)
			}
			if got.APIv4.String() != tt.wantV4 {
				t.Errorf("APIv4: got %q, want %q", got.APIv4, tt.wantV4)
			}
		})
	}
}

func TestConfigGetURLs(t *testing.T) {
	tests := map[string]struct {
		config  Config
		wantV3  string
		wantV4  string
		wantErr bool
	}{
		"baseURLPublic": {
			config: Config{BaseURL: "github.com"},
			wantV3: "https://api.github.com/",
			wantV4: "https://api.github.com/graphql",
		},
		"baseURLEnterprise": {
			config: Config{BaseURL: "https://github.example.com"},
			wantV3: "https://github.example.com/api/v3/",
			wantV4: "https://github.example.com/api/graphql",
		},
		"explicitURLsOverrideBase": {
			config: Config{
				BaseURL:  "https://github.example.com",
				V3APIURL: "https://github.example.com/api/v3/",
				V4APIURL: "https://github.example.com/api/graphql",
				WebURL:   "https://github.example.com",
			},
			wantV3: "https://github.example.com/api/v3/",
			wantV4: "https://github.example.com/api/graphql",
		},
		"noFieldsLeavesURLsUnset": {
			config: Config{},
			wantV3: "",
			wantV4: "",
		},
		"partialOverride": {
			config: Config{
				BaseURL:  "https://github.example.com",
				V3APIURL: "https://custom.example.com/api/v3/",
			},
			wantV3: "https://custom.example.com/api/v3/",
			wantV4: "https://github.example.com/api/graphql",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.config.GetURLs()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotV3 := ""
			if got.APIv3 != nil {
				gotV3 = got.APIv3.String()
			}
			if gotV3 != tt.wantV3 {
				t.Errorf("APIv3: got %q, want %q", gotV3, tt.wantV3)
			}

			gotV4 := ""
			if got.APIv4 != nil {
				gotV4 = got.APIv4.String()
			}
			if gotV4 != tt.wantV4 {
				t.Errorf("APIv4: got %q, want %q", gotV4, tt.wantV4)
			}
		})
	}
}
