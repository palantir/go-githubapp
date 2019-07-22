// Copyright 2019 Palantir Technologies, Inc.
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
	"os"
	"reflect"
	"testing"
)

func TestSetValuesFromEnv(t *testing.T) {
	tests := map[string]struct {
		Input     func(*Config)
		Prefix    string
		Variables map[string]string
		Output    func(*Config)
	}{
		"noVariables": {
			Input: func(c *Config) {
				c.WebURL = "https://github.com"
				c.App.WebhookSecret = "secrethookvalue"
			},
			Output: func(c *Config) {
				c.WebURL = "https://github.com"
				c.App.WebhookSecret = "secrethookvalue"
			},
		},
		"overwriteExisting": {
			Input: func(c *Config) {
				c.WebURL = "https://github.com"
			},
			Variables: map[string]string{
				"GITHUB_WEB_URL": "https://github.company.domain",
			},
			Output: func(c *Config) {
				c.WebURL = "https://github.company.domain"
			},
		},
		"allVariables": {
			Variables: map[string]string{
				"GITHUB_WEB_URL":             "https://github.company.domain",
				"GITHUB_V3_API_URL":          "https://github.company.domain/api/v3",
				"GITHUB_V4_API_URL":          "https://github.company.domain/api/graphql",
				"GITHUB_APP_INTEGRATION_ID":  "4",
				"GITHUB_APP_WEBHOOK_SECRET":  "secrethookvalue",
				"GITHUB_APP_PRIVATE_KEY":     "-----BEGIN RSA PRIVATE KEY-----\nxxx\nxxx\nxxx\n-----END RSA PRIVATE KEY-----",
				"GITHUB_OAUTH_CLIENT_ID":     "92faf4b9146f3278",
				"GITHUB_OAUTH_CLIENT_SECRET": "b00f7ea6d59dd5c9578c48f9391e71db",
			},
			Output: func(c *Config) {
				c.WebURL = "https://github.company.domain"
				c.V3APIURL = "https://github.company.domain/api/v3"
				c.V4APIURL = "https://github.company.domain/api/graphql"
				c.App.IntegrationID = 4
				c.App.WebhookSecret = "secrethookvalue"
				c.App.PrivateKey = "-----BEGIN RSA PRIVATE KEY-----\nxxx\nxxx\nxxx\n-----END RSA PRIVATE KEY-----"
				c.OAuth.ClientID = "92faf4b9146f3278"
				c.OAuth.ClientSecret = "b00f7ea6d59dd5c9578c48f9391e71db"
			},
		},
		"withPrefix": {
			Input: func(c *Config) {
				c.WebURL = "https://github.com"
			},
			Prefix: "TEST_",
			Variables: map[string]string{
				"TEST_GITHUB_WEB_URL": "https://github.company.domain",
			},
			Output: func(c *Config) {
				c.WebURL = "https://github.company.domain"
			},
		},
		"emptyValues": {
			Input: func(c *Config) {
				c.WebURL = "https://github.com"
			},
			Variables: map[string]string{
				"GITHUB_WEB_URL": "",
			},
			Output: func(c *Config) {
				c.WebURL = ""
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			for k, v := range test.Variables {
				if err := os.Setenv(k, v); err != nil {
					t.Fatalf("failed to set environment variable: %s: %v", k, err)
				}
			}

			defer func() {
				for k := range test.Variables {
					if err := os.Unsetenv(k); err != nil {
						t.Fatalf("failed to clear environment variable: %s: %v", k, err)
					}
				}
			}()

			var in Config
			if test.Input != nil {
				test.Input(&in)
			}

			var out Config
			if test.Output != nil {
				test.Output(&out)
			}

			in.SetValuesFromEnv(test.Prefix)

			if !reflect.DeepEqual(out, in) {
				t.Errorf("incorrect configuration\nexpected: %+v\n  actual: %+v", out, in)
			}
		})
	}
}
