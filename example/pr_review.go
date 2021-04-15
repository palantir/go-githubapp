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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-github/v33/github"
	"github.com/palantir/go-githubapp/githubapp"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	transport_http "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

type PRReviewHandler struct {
	githubapp.ClientCreator

	preamble string
}

func (h *PRReviewHandler) Handles() []string {
	return []string{"pull_request"}
}

func (h *PRReviewHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event github.IssueCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}

	if !event.GetIssue().IsPullRequest() {
		zerolog.Ctx(ctx).Debug().Msg("Issue comment event is not for a pull request")
		return nil
	}

	repo := event.GetRepo()
	prNum := event.GetIssue().GetNumber()
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, repo, event.GetIssue().GetNumber())

	logger.Debug().Msgf("Event action is %s", event.GetAction())
	if event.GetAction() != "created" {
		return nil
	}

	// Get Access Token
	client, ts, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}
	token, err := ts.Token(context.Background())

	// Clone the repository
	tokenAuth := &transport_http.BasicAuth{Username: "x-access-token", Password: token}
	storer := memory.NewStorage()
	gitRepo, err := git.Clone(storer, nil, &git.CloneOptions{
		URL:  "https://github.com/palantir/go-githubapp.git",
		Auth: tokenAuth,
	})

	// Insert your own advanced Git scenario here:
	mainRef, _ := gitRepo.Reference(plumbing.NewBranchReferenceName(event.GetRepo().GetMasterBranch()), true)
	commit, _ := gitRepo.CommitObject(mainRef.Hash())
	logger.Debug().Msgf("Last commit on master was by %s", commit.Author.Name)

	// Remainder of old logic...
	repoOwner := repo.GetOwner().GetLogin()
	repoName := repo.GetName()
	author := event.GetComment().GetUser().GetLogin()
	body := event.GetComment().GetBody()

	if strings.HasSuffix(author, "[bot]") {
		logger.Debug().Msg("Issue comment was created by a bot")
		return nil
	}

	logger.Debug().Msgf("Echoing comment on %s/%s#%d by %s", repoOwner, repoName, prNum, author)
	msg := fmt.Sprintf("%s\n%s said\n```\n%s\n```\n", h.preamble, author, body)
	prComment := github.IssueComment{
		Body: &msg,
	}

	if _, _, err := client.Issues.CreateComment(ctx, repoOwner, repoName, prNum, &prComment); err != nil {
		logger.Error().Err(err).Msg("Failed to comment on pull request")
	}

	return nil
}
