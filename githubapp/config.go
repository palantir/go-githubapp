// Copyright 2018 Palantir Technologies, Inc.
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
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	// BaseURL is a convenience field that sets Web, V3APIURL, and V4APIURL
	// automatically via ParseURLs. Explicit URL fields take precedence.
	BaseURL  string `yaml:"base_url" json:"baseUrl"`
	WebURL   string `yaml:"web_url" json:"webUrl"`
	V3APIURL string `yaml:"v3_api_url" json:"v3ApiUrl"`
	V4APIURL string `yaml:"v4_api_url" json:"v4ApiUrl"`

	App struct {
		IntegrationID int64  `yaml:"integration_id" json:"integrationId"`
		WebhookSecret string `yaml:"webhook_secret" json:"webhookSecret"`
		PrivateKey    string `yaml:"private_key" json:"privateKey"`
	} `yaml:"app" json:"app"`

	OAuth struct {
		ClientID     string `yaml:"client_id" json:"clientId"`
		ClientSecret string `yaml:"client_secret" json:"clientSecret"`
	} `yaml:"oauth" json:"oauth"`
}

// SetValuesFromEnv sets configuration values from environment variables.
// The optional prefix is prepended to each variable name.
func (c *Config) SetValuesFromEnv(prefix string) {
	setStringFromEnv("GITHUB_BASE_URL", prefix, &c.BaseURL)
	setStringFromEnv("GITHUB_WEB_URL", prefix, &c.WebURL)
	setStringFromEnv("GITHUB_V3_API_URL", prefix, &c.V3APIURL)
	setStringFromEnv("GITHUB_V4_API_URL", prefix, &c.V4APIURL)

	setIntFromEnv("GITHUB_APP_INTEGRATION_ID", prefix, &c.App.IntegrationID)
	setStringFromEnv("GITHUB_APP_WEBHOOK_SECRET", prefix, &c.App.WebhookSecret)
	setStringFromEnv("GITHUB_APP_PRIVATE_KEY", prefix, &c.App.PrivateKey)

	setStringFromEnv("GITHUB_OAUTH_CLIENT_ID", prefix, &c.OAuth.ClientID)
	setStringFromEnv("GITHUB_OAUTH_CLIENT_SECRET", prefix, &c.OAuth.ClientSecret)
}

// GetURLs resolves the GitHub endpoint URLs for this configuration.
// Explicit URL fields (WebURL, V3APIURL, V4APIURL) take precedence over
// BaseURL. If none are set, it falls back to ParseURLs("github.com").
func (c *Config) GetURLs() (*URLs, error) {
	// all three explicit URLs are set — use them directly
	if c.WebURL != "" && c.V3APIURL != "" && c.V4APIURL != "" {
		return parseExplicitURLs(c.WebURL, c.V3APIURL, c.V4APIURL)
	}

	// derive from BaseURL, then overlay any explicit overrides
	base := c.BaseURL
	if base == "" {
		base = githubPublicHost
	}
	urls, err := ParseURLs(base)
	if err != nil {
		return nil, err
	}

	if c.WebURL != "" {
		if urls.Web, err = url.Parse(c.WebURL); err != nil {
			return nil, err
		}
	}
	if c.V3APIURL != "" {
		if urls.APIv3, err = url.Parse(c.V3APIURL); err != nil {
			return nil, err
		}
	}
	if c.V4APIURL != "" {
		if urls.APIv4, err = url.Parse(c.V4APIURL); err != nil {
			return nil, err
		}
	}
	return urls, nil
}

func parseExplicitURLs(web, v3, v4 string) (*URLs, error) {
	wu, err := url.Parse(web)
	if err != nil {
		return nil, err
	}
	v3u, err := url.Parse(v3)
	if err != nil {
		return nil, err
	}
	v4u, err := url.Parse(v4)
	if err != nil {
		return nil, err
	}
	return &URLs{Web: wu, APIv3: v3u, APIv4: v4u}, nil
}

func setStringFromEnv(key, prefix string, value *string) {
	if v, ok := os.LookupEnv(prefix + key); ok {
		*value = v
	}
}

func setIntFromEnv(key, prefix string, value *int64) {
	if v, ok := os.LookupEnv(prefix + key); ok {
		if i, err := strconv.ParseInt(v, 10, 0); err == nil {
			*value = i
		}
	}
}
