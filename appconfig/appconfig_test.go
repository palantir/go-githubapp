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
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v38/github"
)

const (
	TestOwner = "test"
	TestRef   = "develop"
)

func TestLoadConfig(t *testing.T) {
	tests := map[string]struct {
		Paths    []string
		Options  []Option
		Repo     string
		Expected Config
		Error    bool
	}{
		"localFile": {
			Paths: []string{".github/test-app.yml"},
			Repo:  "local-file",
			Expected: Config{
				Content: []byte("message: hello\n"),
				Source:  "test/local-file@develop",
				Path:    ".github/test-app.yml",
			},
		},
		"localFileFallback": {
			Paths: []string{".github/test-app.v2.yml", ".github/test-app.yml"},
			Repo:  "local-file",
			Expected: Config{
				Content: []byte("message: hello\n"),
				Source:  "test/local-file@develop",
				Path:    ".github/test-app.yml",
			},
		},
		"localFileLarge": {
			Paths: []string{".github/test-app.yml"},
			Repo:  "local-file-large",
			Expected: Config{
				Content: []byte("message: hello\n"),
				Source:  "test/local-file-large@develop",
				Path:    ".github/test-app.yml",
			},
		},
		"remoteReference": {
			Paths: []string{".github/test-app.yml"},
			Repo:  "remote-ref",
			Expected: Config{
				Content:  []byte("message: hello\n"),
				Source:   "remote/config@develop",
				Path:     "config/test-app.yml",
				IsRemote: true,
			},
		},
		"remoteReferenceEmptyGitRef": {
			Paths: []string{".github/test-app.yml"},
			Repo:  "remote-ref-empty-git-ref",
			Expected: Config{
				Content:  []byte("message: hello\n"),
				Source:   "remote/config@main",
				Path:     "config/test-app.yml",
				IsRemote: true,
			},
		},
		"defaultConfig": {
			Paths: []string{".github/test-app.yml"},
			Repo:  "default-config",
			Expected: Config{
				Content: []byte("message: hello\n"),
				Source:  "test/.github@develop",
				Path:    "test-app.yml",
			},
		},
		"defaultConfigRemoteReference": {
			Paths: []string{".github-remote/test-app.yml"},
			Options: []Option{
				WithOwnerDefault(".github-remote", []string{"test-app.yml"}),
			},
			Repo: "default-config-remote-ref",
			Expected: Config{
				Content:  []byte("message: hello\n"),
				Source:   "remote/config@develop",
				Path:     "config/test-app.yml",
				IsRemote: true,
			},
		},
	}

	ctx := context.Background()
	client := makeTestClient()

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ld := NewLoader(test.Paths, test.Options...)

			cfg, err := ld.LoadConfig(ctx, client, TestOwner, test.Repo, TestRef)
			if test.Error {
				if err == nil {
					t.Fatal("expected error loading config, but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error loading config: %v", err)
			}

			if test.Expected.Source != cfg.Source {
				t.Errorf("incorrect source: expected: %q, actual: %q", test.Expected.Source, cfg.Source)
			}
			if test.Expected.Path != cfg.Path {
				t.Errorf("incorrect path: expected: %q, actual: %q", test.Expected.Path, cfg.Path)
			}
			if test.Expected.IsRemote != cfg.IsRemote {
				t.Errorf("incorrect remote flag: expected: %t, actual: %t", test.Expected.IsRemote, cfg.IsRemote)
			}
			if !bytes.Equal(test.Expected.Content, cfg.Content) {
				t.Errorf("incorrect content\nexpected: %s\n  actual: %s", test.Expected.Content, cfg.Content)
			}
		})
	}
}

func makeTestClient() *github.Client {
	rp := &ResponsePlayer{}
	for route, f := range map[string]string{
		"/repos/test/local-file/contents/.github/test-app.yml":    "local-file-contents.yml",
		"/repos/test/local-file/contents/.github/test-app.v2.yml": "404.yml",

		"/repos/test/local-file-large/contents/.github/test-app.yml": "local-file-large-contents.yml",
		"/repos/test/local-file-large/contents/.github":              "local-file-large-dir-contents.yml",
		"/test/local-file-large/develop/.github/test-app.yml":        "local-file-large-download.yml",

		"/repos/test/remote-ref/contents/.github/test-app.yml":               "remote-ref-contents.yml",
		"/repos/test/remote-ref-empty-git-ref/contents/.github/test-app.yml": "remote-ref-empty-git-ref-contents.yml",
		"/repos/remote/config/contents/config/test-app.yml":                  "config-contents.yml",
		"/repos/remote/config": "remote-config.yml",

		"/repos/test/default-config/contents/.github/test-app.yml": "404.yml",
		"/repos/test/.github":                       "dot-github.yml",
		"/repos/test/.github/contents/test-app.yml": "dot-github-contents.yml",

		"/repos/test/default-config-remote-ref/contents/.github-remote/test-app.yml": "404.yml",
		"/repos/test/.github-remote":               "remote-config.yml",
		"/repos/test/config/contents/test-app.yml": "remote-ref-contents.yml",
	} {
		rp.AddRule(ExactPathMatcher(route), filepath.Join("testdata", f))
	}
	return github.NewClient(&http.Client{Transport: rp})
}
