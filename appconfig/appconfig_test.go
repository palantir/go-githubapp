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
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v91/github"
	"github.com/palantir/go-githubapp/githubapp"
)

const (
	TestOwner = "test"
	TestRef   = "develop"
)

type testInstallationsService struct {
	githubapp.InstallationsService

	installation githubapp.Installation
	err          error
	calls        int
	owner        string
	repo         string
}

func (s *testInstallationsService) GetByRepository(_ context.Context, owner, repo string) (githubapp.Installation, error) {
	s.calls++
	s.owner = owner
	s.repo = repo
	if s.err != nil {
		return githubapp.Installation{}, s.err
	}
	return s.installation, nil
}

type testClientCreator struct {
	githubapp.ClientCreator

	client         *github.Client
	err            error
	calls          int
	installationID int64
}

func (c *testClientCreator) NewInstallationClient(installationID int64) (*github.Client, error) {
	c.calls++
	c.installationID = installationID
	return c.client, c.err
}

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

func TestLoadConfigWithPrivateRemotes(t *testing.T) {
	newClient := func(rp *ResponsePlayer) *github.Client {
		client, _ := github.NewClient(github.WithHTTPClient(&http.Client{Transport: rp}))
		return client
	}
	localRules := func(includeRemote bool) *ResponsePlayer {
		rp := &ResponsePlayer{}
		rp.AddRule(ExactPathMatcher("/repos/test/remote-ref/contents/.github/test-app.yml"), filepath.Join("testdata", "remote-ref-contents.yml"))
		if includeRemote {
			rp.AddRule(ExactPathMatcher("/repos/remote/config/contents/config/test-app.yml"), filepath.Join("testdata", "config-contents.yml"))
		}
		return rp
	}

	t.Run("uses remote repository installation client", func(t *testing.T) {
		localRules := localRules(false)
		remoteRules := &ResponsePlayer{}
		remoteRule := remoteRules.AddRule(ExactPathMatcher("/repos/remote/config/contents/config/test-app.yml"), filepath.Join("testdata", "config-contents.yml"))
		installations := &testInstallationsService{installation: githubapp.Installation{ID: 42, Owner: "remote"}}
		creator := &testClientCreator{client: newClient(remoteRules)}

		loader := NewLoader([]string{".github/test-app.yml"}, WithPrivateRemotes(creator, installations))
		cfg, err := loader.LoadConfig(context.Background(), newClient(localRules), TestOwner, "remote-ref", TestRef)
		if err != nil {
			t.Fatalf("unexpected error loading config: %v", err)
		}
		if !bytes.Equal(cfg.Content, []byte("message: hello\n")) {
			t.Errorf("incorrect content: %s", cfg.Content)
		}
		if installations.calls != 1 || creator.calls != 1 || remoteRule.Count != 1 {
			t.Errorf("expected one installation lookup, client creation, and remote request; got %d, %d, and %d", installations.calls, creator.calls, remoteRule.Count)
		}
		if installations.owner != "remote" || installations.repo != "config" {
			t.Errorf("expected installation lookup for remote/config, got %s/%s", installations.owner, installations.repo)
		}
		if creator.installationID != 42 {
			t.Errorf("expected installation client for ID 42, got %d", creator.installationID)
		}
	})

	for name, test := range map[string]struct {
		installationsErr error
		creatorErr       error
	}{
		"falls back when remote repository has no installation": {installationsErr: githubapp.InstallationNotFound("remote/config")},
		"falls back when installation client creation fails":    {creatorErr: errors.New("client creation failed")},
	} {
		t.Run(name, func(t *testing.T) {
			localRules := localRules(true)
			installations := &testInstallationsService{installation: githubapp.Installation{ID: 42, Owner: "remote"}, err: test.installationsErr}
			creator := &testClientCreator{err: test.creatorErr}

			loader := NewLoader([]string{".github/test-app.yml"}, WithPrivateRemotes(creator, installations))
			cfg, err := loader.LoadConfig(context.Background(), newClient(localRules), TestOwner, "remote-ref", TestRef)
			if err != nil {
				t.Fatalf("unexpected error loading config: %v", err)
			}
			if !bytes.Equal(cfg.Content, []byte("message: hello\n")) {
				t.Errorf("incorrect content: %s", cfg.Content)
			}
			if test.installationsErr != nil && creator.calls != 0 {
				t.Errorf("client creator was called after installation lookup failed")
			}
			if test.creatorErr != nil && creator.calls != 1 {
				t.Errorf("expected one client creation attempt, got %d", creator.calls)
			}
		})
	}
}

func TestPrivateRemotesSameOwnerReusesOriginalClient(t *testing.T) {
	client := makeTestClient()
	installations := &testInstallationsService{}
	creator := &testClientCreator{}
	loader := NewLoader(nil, WithPrivateRemotes(creator, installations))

	got := loader.remoteClient(context.Background(), client, "Source-Owner", "source-owner", "config")
	if got != client {
		t.Error("same-owner remote did not reuse the original client")
	}
	if installations.calls != 0 || creator.calls != 0 {
		t.Errorf("same-owner remote attempted installation lookup or client creation: %d, %d", installations.calls, creator.calls)
	}
}

func makeTestClient() *github.Client {
	rp := &ResponsePlayer{}
	for route, f := range map[string]string{
		"/repos/test/local-file/contents/.github/test-app.yml":    "local-file-contents.yml",
		"/repos/test/local-file/contents/.github/test-app.v2.yml": "404.yml",

		"/repos/test/local-file-large/contents/.github/test-app.yml": "local-file-large-contents.yml",
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
	client, _ := github.NewClient(github.WithHTTPClient(&http.Client{Transport: rp}))
	return client
}
