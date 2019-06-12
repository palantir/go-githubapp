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
	"net/http"

	"github.com/google/go-github/github"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/palantir/go-githubapp/githubapp"
)

type PRCommentHandler struct {
	githubapp.ClientCreator

	preamble string
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"issue_comment"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte, w http.ResponseWriter) (int, []byte, error) {
	var event github.IssueCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return http.StatusBadRequest, nil, errors.Wrap(err, "failed to parse issue comment event payload")
	}

	if !event.GetIssue().IsPullRequest() {
		zerolog.Ctx(ctx).Debug().Msg("Issue comment event is not for a pull request")
		return 0, nil, nil
	}

	repo := event.GetRepo()
	prNum := event.GetIssue().GetNumber()
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, repo, event.GetIssue().GetNumber())

	logger.Debug().Msgf("Event action is %s", event.GetAction())
	if event.GetAction() != "created" {
		return 0, nil, nil
	}

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	repoOwner := repo.GetOwner().GetLogin()
	repoName := repo.GetName()
	author := event.GetComment().GetUser()
	body := event.GetComment().GetBody()

	logger.Debug().Msgf("Echoing comment on %s/%s#%d by %s", repoOwner, repoName, prNum, author)
	msg := fmt.Sprintf("%s\n%s said\n```%s\n```\n", h.preamble, author, body)
	prComment := github.PullRequestComment{
		Body: &msg,
	}

	if _, _, err := client.PullRequests.CreateComment(ctx, repoOwner, repoName, prNum, &prComment); err != nil {
		logger.Error().Err(err).Msg("Failed to comment on pull request")
	}

	return 0, nil, nil
}
