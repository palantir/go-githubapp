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
	"fmt"
	"net/url"
	"strings"
)

const (
	githubPublicHost = "github.com"

	githubPublicV3URL  = "https://api.github.com/"
	githubPublicV4URL  = "https://api.github.com/graphql"
	githubPublicWebURL = "https://github.com"
)

// URLs holds the resolved endpoint URLs for a GitHub deployment.
type URLs struct {
	Web   *url.URL
	APIv3 *url.URL
	APIv4 *url.URL
}

// ParseURLs resolves all GitHub endpoint URLs from a single base URL.
// For github.com it returns the known public API endpoints; for any other
// host it constructs the standard GitHub Enterprise Server paths.
func ParseURLs(baseURL string) (*URLs, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("githubapp: base URL must not be empty")
	}

	// normalize: add scheme if missing so url.Parse works reliably
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("githubapp: invalid base URL %q: %w", baseURL, err)
	}

	host := strings.ToLower(u.Hostname())
	if host == githubPublicHost {
		return publicURLs()
	}
	return enterpriseURLs(u)
}

func publicURLs() (*URLs, error) {
	web, _ := url.Parse(githubPublicWebURL)
	v3, _ := url.Parse(githubPublicV3URL)
	v4, _ := url.Parse(githubPublicV4URL)
	return &URLs{Web: web, APIv3: v3, APIv4: v4}, nil
}

func enterpriseURLs(base *url.URL) (*URLs, error) {
	// strip any path so we build from the root of the host
	root := &url.URL{Scheme: base.Scheme, Host: base.Host}

	v3, err := root.Parse("api/v3/")
	if err != nil {
		return nil, fmt.Errorf("githubapp: could not construct v3 API URL: %w", err)
	}

	v4, err := root.Parse("api/graphql")
	if err != nil {
		return nil, fmt.Errorf("githubapp: could not construct v4 API URL: %w", err)
	}

	web := &url.URL{Scheme: root.Scheme, Host: root.Host, Path: "/"}

	return &URLs{Web: web, APIv3: v3, APIv4: v4}, nil
}
