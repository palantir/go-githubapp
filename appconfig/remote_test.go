// Copyright 2026 Palantir Technologies, Inc.
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
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/palantir/go-githubapp/githubapp"
)

type remoteRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f remoteRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newRemoteTestClient(t *testing.T, rt http.RoundTripper) *github.Client {
	t.Helper()
	client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestLoadRemoteRefLiteral(t *testing.T) {
	content := "remote: remote/config\npath: first.yml\nref: main\n"
	var requests []string
	client := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.RequestURI())
		response := SavedResponse{Status: http.StatusOK, Body: `{"default_branch":"main"}`}
		if req.URL.Path != "/repos/remote/config" {
			response.Body = `{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(content)) + `"}`
		}
		return response.Response(req), nil
	}))
	loader := NewLoader([]string{"first.yml", "second.yml"}, WithRemoteRefParser(func(string, []byte) (*RemoteRef, error) {
		t.Error("literal loading invoked the remote parser")
		return nil, errors.New("unexpected parser call")
	}))

	cfg, err := loader.LoadRemoteRef(context.Background(), client, "original", RemoteRef{Remote: "remote/config"})
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Content: []byte(content), Source: "remote/config@main", Path: "first.yml", IsRemote: true}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
	wantRequests := []string{"/repos/remote/config", "/repos/remote/config/contents/first.yml?ref=main"}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Errorf("requests: got %v, want %v", requests, wantRequests)
	}
}

func TestLoadRemoteRefMissingAndEmpty(t *testing.T) {
	for name, test := range map[string]struct {
		response SavedResponse
		missing  bool
	}{
		"missing target": {response: SavedResponse{Status: http.StatusNotFound}, missing: true},
		"directory":      {response: SavedResponse{Status: http.StatusOK, Body: `[]`}, missing: true},
		"empty file":     {response: SavedResponse{Status: http.StatusOK, Body: `{"encoding":"base64","content":""}`}},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.RequestURI() != "/repos/remote/config/contents/first.yml?ref=main" {
					t.Errorf("unexpected request: %s", req.URL)
				}
				return test.response.Response(req), nil
			}))
			loader := NewLoader([]string{"first.yml", "second.yml"})
			cfg, err := loader.LoadRemoteRef(context.Background(), client, "original", RemoteRef{Remote: "remote/config", Ref: "main"})
			if test.missing {
				if err == nil || err.Error() != "invalid remote reference: file does not exist" {
					t.Fatalf("expected missing remote error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || cfg.IsUndefined() || len(cfg.Content) != 0 || cfg.Source != "remote/config@main" || cfg.Path != "first.yml" || !cfg.IsRemote {
				t.Errorf("unexpected config or request count: %+v, %d", cfg, calls)
			}
		})
	}
}

func TestLoadRemoteRefInvalidRepository(t *testing.T) {
	for _, remote := range []string{
		"", "owner", "/repo", "owner/", "owner/repo/extra",
		"owner/repo#", "owner/repo?x=", "owner/repo/contents/other.yml#",
		"owner#fragment/repo", "owner%2frepo/name", "owner/repo%23fragment",
		"./repo", "../repo", "owner/.", "owner/..", "owner/%2e%2e",
		"https://example.invalid/repo", "owner/repo\\extra", "owner/repo name",
	} {
		t.Run(remote, func(t *testing.T) {
			client := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				t.Error("invalid remote made an HTTP request")
				// Query/fragment injection previously made this look like an empty file.
				return (&SavedResponse{Status: http.StatusOK, Body: `{"name":"repo"}`}).Response(req), nil
			}))
			installations := &testInstallationsService{}
			creator := &testClientCreator{client: client}
			loader := NewLoader(nil, WithPrivateRemotes(creator, installations))
			cfg, err := loader.LoadRemoteRef(context.Background(), client, "original", RemoteRef{Remote: remote, Path: "app.yml", Ref: "main"})
			if err == nil || !reflect.DeepEqual(cfg, Config{}) {
				t.Errorf("expected invalid remote error and empty Config, got %+v, %v", cfg, err)
			}
			if installations.calls != 0 || creator.calls != 0 {
				t.Errorf("invalid remote triggered installation lookup/client creation: %d, %d", installations.calls, creator.calls)
			}
		})
	}
}

func TestLoadRemoteRefSuccessivePrivateFiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		t.Error("used original client for private remote")
		return errorResponse(req, http.StatusForbidden, "denied")
	}))
	var requests []string
	selected := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		if req.Context() != ctx {
			t.Error("selected client lost caller context")
		}
		response := SavedResponse{Status: http.StatusOK}
		switch req.URL.Path {
		case "/repos/remote/config/contents/child.yml":
			response.Body = `{"encoding":"base64","content":"Y2hpbGQ="}`
		case "/repos/remote/config/contents/parent.yml":
			response.Body = `{"encoding":"none","download_url":"https://download.example/parent"}`
		case "/parent":
			response.Body = "parent"
		}
		return response.Response(req), nil
	}))
	installations := &testInstallationsService{installation: githubapp.Installation{ID: 42}}
	creator := &testClientCreator{client: selected}
	loader := NewLoader([]string{"unused.yml"}, WithPrivateRemotes(creator, installations))

	for _, file := range []string{"child", "parent"} {
		cfg, err := loader.LoadRemoteRef(ctx, original, "source", RemoteRef{Remote: "remote/config", Path: file + ".yml", Ref: "main"})
		if err != nil || string(cfg.Content) != file || cfg.Source != "remote/config@main" || cfg.Path != file+".yml" || !cfg.IsRemote {
			t.Fatalf("incorrect remote config: %+v, %v", cfg, err)
		}
	}
	if installations.calls != 2 || installations.owner != "remote" || installations.repo != "config" || creator.calls != 2 || creator.installationID != 42 {
		t.Errorf("incorrect installation selection: lookup=%+v creator=%+v", installations, creator)
	}
	wantRequests := []string{
		"https://api.github.com/repos/remote/config/contents/child.yml?ref=main",
		"https://api.github.com/repos/remote/config/contents/parent.yml?ref=main",
		"https://download.example/parent",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Errorf("requests: got %v, want %v", requests, wantRequests)
	}
}

func TestLoadRemoteRefAccessFailures(t *testing.T) {
	for name, test := range map[string]struct {
		lookupErr, creatorErr      error
		originalStatus             int
		originalCalls, clientCalls int
		creations                  int
	}{
		"lookup failure":   {lookupErr: githubapp.InstallationNotFound("remote/config"), originalStatus: http.StatusForbidden, originalCalls: 1},
		"creation failure": {creatorErr: errors.New("creation failed"), originalStatus: http.StatusForbidden, originalCalls: 1, creations: 1},
		"read failure":     {originalStatus: http.StatusOK, clientCalls: 1, creations: 1},
	} {
		t.Run(name, func(t *testing.T) {
			originalCalls, selectedCalls := 0, 0
			original := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				originalCalls++
				return (&SavedResponse{Status: test.originalStatus, Body: `{"encoding":"base64","content":""}`}).Response(req), nil
			}))
			selected := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				selectedCalls++
				return errorResponse(req, http.StatusForbidden, "denied")
			}))
			installations := &testInstallationsService{installation: githubapp.Installation{ID: 42}, err: test.lookupErr}
			creator := &testClientCreator{client: selected, err: test.creatorErr}
			loader := NewLoader(nil, WithPrivateRemotes(creator, installations))
			_, err := loader.LoadRemoteRef(context.Background(), original, "source", RemoteRef{Remote: "remote/config", Path: "app.yml", Ref: "main"})
			var responseErr *github.ErrorResponse
			if !errors.As(err, &responseErr) || responseErr.Response.StatusCode != http.StatusForbidden {
				t.Fatalf("expected access denial, got %v", err)
			}
			if originalCalls != test.originalCalls || selectedCalls != test.clientCalls || installations.calls != 1 || creator.calls != test.creations {
				t.Errorf("incorrect access attempts: original=%d selected=%d lookup=%d creation=%d", originalCalls, selectedCalls, installations.calls, creator.calls)
			}
		})
	}
}

func TestLoadConfigInvalidRemoteDiagnostics(t *testing.T) {
	content := "remote: remote/config#\n"
	client := newRemoteTestClient(t, remoteRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		response := SavedResponse{Status: http.StatusOK}
		if req.URL.Path == "/repos/original/.github" {
			response.Body = `{"name":".github","default_branch":"main"}`
		} else {
			response.Body = `{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(content)) + `"}`
		}
		return response.Response(req), nil
	}))
	for _, test := range []struct {
		name, source string
		paths        []string
	}{
		{name: "local", source: "original/local@main", paths: []string{"app.yml"}},
		{name: "owner default", source: "original/.github@main"},
	} {
		t.Run(test.name, func(t *testing.T) {
			loader := NewLoader(test.paths, WithOwnerDefault(".github", []string{"app.yml"}))
			cfg, err := loader.LoadConfig(context.Background(), client, "original", "local", "main")
			if err == nil || err.Error() != "invalid remote value: remote/config#" || cfg.Source != test.source || cfg.Path != "app.yml" || cfg.IsRemote {
				t.Fatalf("lost originating file diagnostics: %+v, %v", cfg, err)
			}
		})
	}
}
