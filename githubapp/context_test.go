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
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-github/v65/github"
	"github.com/rs/zerolog"
)

func TestPrepareRepoContext(t *testing.T) {
	var out bytes.Buffer

	logger := zerolog.New(&out)
	ctx := logger.WithContext(context.Background())

	_, logger = PrepareRepoContext(ctx, 42, &github.Repository{
		Name: github.String("test"),
		Owner: &github.User{
			Login: github.String("mhaypenny"),
		},
	})

	logger.Info().Msg("")

	var entry struct {
		ID    int64  `json:"github_installation_id"`
		Owner string `json:"github_repository_owner"`
		Name  string `json:"github_repository_name"`
	}
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatalf("invalid log entry: %s: %v", out.String(), err)
	}

	assertField(t, "installation ID", int64(42), entry.ID)
	assertField(t, "repository owner", "mhaypenny", entry.Owner)
	assertField(t, "repository name", "test", entry.Name)
}

func TestPreparePRContext(t *testing.T) {
	var out bytes.Buffer

	logger := zerolog.New(&out)
	ctx := logger.WithContext(context.Background())

	_, logger = PreparePRContext(ctx, 42, &github.Repository{
		Name: github.String("test"),
		Owner: &github.User{
			Login: github.String("mhaypenny"),
		},
	}, 128)

	logger.Info().Msg("")

	var entry struct {
		ID     int64  `json:"github_installation_id"`
		Owner  string `json:"github_repository_owner"`
		Name   string `json:"github_repository_name"`
		Number int    `json:"github_pr_num"`
	}
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatalf("invalid log entry: %s: %v", out.String(), err)
	}

	assertField(t, "installation ID", int64(42), entry.ID)
	assertField(t, "repository owner", "mhaypenny", entry.Owner)
	assertField(t, "repository name", "test", entry.Name)
	assertField(t, "pull request number", 128, entry.Number)
}

func assertField(t *testing.T, name string, expected, actual interface{}) {
	if expected != actual {
		t.Errorf("incorrect %s: expected %#v (%T), but was %#v (%T)", name, expected, expected, actual, actual)
	}
}
