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
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-github/v58/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/redhat-appstudio/qe-tools/pkg/prow"
	"github.com/rs/zerolog"
	"k8s.io/apimachinery/pkg/util/wait"

	reporters "github.com/onsi/ginkgo/v2/reporters"
)

const (
	targetAuthor  = "dheerajodha"
	junitFilename = `/(j?unit).*\.xml`
	openshiftCITestSuiteName = "openshift-ci job"
	e2eTestSuiteName = "Red Hat App Studio E2E tests"
	regexToFetchProwURL = `(https:\/\/prow.ci.openshift.org\/view\/gs\/test-platform-results\/pr-logs\/pull.*)\)`
)

type PRCommentHandler struct {
	githubapp.ClientCreator

	preamble string
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"issue_comment"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
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

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	repoOwner := repo.GetOwner().GetLogin()
	repoName := repo.GetName()
	author := event.GetComment().GetUser().GetLogin()
	commentID := event.GetComment().GetID()
	body := event.GetComment().GetBody()

	if !strings.HasPrefix(author, targetAuthor) {
		logger.Debug().Msg(fmt.Sprintf("Issue comment was not created by the user: %s", targetAuthor))
		return nil
	}

	// fetch the prow URL
	r, _ := regexp.Compile(regexToFetchProwURL)
	sliceOfMatchingString := r.FindStringSubmatch(body)
	if sliceOfMatchingString == nil {
		return fmt.Errorf("regex string %s found no match for the string: %s", regexToFetchProwURL, body)
	}
	prowJobURL := sliceOfMatchingString[1]
	logger.Debug().Msgf("Prow Job's URL: %s", prowJobURL)

	// process the test failures from the prow URL
	cfg := prow.ScannerConfig{
		ProwJobURL:      prowJobURL,
		FileNameFilter: []string{junitFilename}, // cross check its targets only the junit.xml within the ....
	}

	scanner, err := prow.NewArtifactScanner(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize artifact scanner: %+v", err)
	}

	if err := scanner.Run(); err != nil {
		return fmt.Errorf("failed to scan artifacts for prow job %s: %+v", prowJobURL, err)
	}

	failedTestCasesNames := getFailedTestCases(scanner, logger)

	// Update the comment body with the names of failed testcases
	if len(failedTestCasesNames) > 0 {
		logger.Debug().Msgf("Updating comment with ID:%d %s/%s#%d by %s", commentID, repoOwner, repoName, prNum, author)

		msg := "**List of E2E tests that failed in the latest CI run**: \n"
		for _, testcaseName := range failedTestCasesNames {
			msg = msg + fmt.Sprintf("\n* %s\n", testcaseName)
		}
		msg = msg + "\n-------------------------------\n\n" + body

		prComment := github.IssueComment{
			Body: &msg,
		}

		err := wait.PollUntilContextTimeout(context.Background(), 3*time.Second, 10*time.Minute, true, func(context.Context) (done bool, err error) {
			if _, _, err := client.Issues.EditComment(ctx, repoOwner, repoName, commentID, &prComment); err != nil {
				logger.Error().Err(err).Msg("Failed to edit comment...retrying")
				return false, nil
			}

			return true, nil
		})

		if err != nil {
			logger.Error().Err(err).Msg(fmt.Sprintf("Failed to edit comment, will stop processing this comment with ID: %v", commentID))
		}
	}

	return nil
}

func getFailedTestCases(scanner *prow.ArtifactScanner, logger zerolog.Logger) []string {
	failedTestCasesNames := []string{}

	overallJUnitSuites := &reporters.JUnitTestSuites{}

	for _, artifactsFilenameMap := range scanner.ArtifactStepMap {
		for artifactFilename, artifact := range artifactsFilenameMap {
			if artifactFilename == "junit.xml" {
				logger.Debug().Msgf("Processing file name: %s", artifactFilename)
				if err := xml.Unmarshal([]byte(artifact.Content), overallJUnitSuites); err != nil {
					logger.Error().Err(err).Msg("cannot decode JUnit suite into xml")
				}
				logger.Debug().Msgf(fmt.Sprintf("%s", overallJUnitSuites))

				if len(overallJUnitSuites.TestSuites) == 1 && overallJUnitSuites.TestSuites[0].Name == openshiftCITestSuiteName {
					logger.Error().Msg(fmt.Sprintf("junit.xml only contains 1 TestSuite with name: %s", openshiftCITestSuiteName))
					return append(failedTestCasesNames, "Test Job failed during the Setup phase")
				}
			}
		}
	}

	for _, testSuite := range overallJUnitSuites.TestSuites {
		if testSuite.Name == e2eTestSuiteName && (testSuite.Failures > 0 || testSuite.Errors > 0) {
			for _, testCase := range testSuite.TestCases {
				if testCase.Failure != nil || testCase.Error != nil {
					logger.Debug().Msgf("Failed Test Case name: %s", testCase.Name)
					failedTestCasesNames = append(failedTestCasesNames, testCase.Name)
				}
			}
		}
	}

	return failedTestCasesNames
}
