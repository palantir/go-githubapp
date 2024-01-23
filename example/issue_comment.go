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
	"k8s.io/klog/v2"

	reporters "github.com/onsi/ginkgo/v2/reporters"
)

const (
	targetAuthor             = "dheerajodha"
	junitFilename            = "junit.xml"
	junitFilenameRegex       = `(junit.xml)`
	openshiftCITestSuiteName = "openshift-ci job"
	e2eTestSuiteName         = "Red Hat App Studio E2E tests"
	regexToFetchProwURL      = `(https:\/\/prow.ci.openshift.org\/view\/gs\/test-platform-results\/pr-logs\/pull.*)\)`
)

type PRCommentHandler struct {
	githubapp.ClientCreator

	preamble string
}

type FailedTestCasesReport struct {
	headerString        string
	failedTestCaseNames     []string
	hasBootstrapFailure bool
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"issue_comment"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event github.IssueCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}

	if !event.GetIssue().IsPullRequest() || event.GetAction() != "created" {
		return nil
	}

	installationID := githubapp.GetInstallationIDFromEvent(&event)

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, event.GetRepo(), event.GetIssue().GetNumber())

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	author := event.GetComment().GetUser().GetLogin()
	body := event.GetComment().GetBody()

	if !strings.HasPrefix(author, targetAuthor) {
		klog.Infof("Issue comment was not created by the user: %s. Ignoring this comment", targetAuthor)
		return nil
	}

	// extract the Prow job's URL
	prowJobURL, err := extractProwJobURLFromCommentBody(logger, body)
	if err != nil {
		return fmt.Errorf("unable to extract Prow job's URL from the PR comment's body: %+v", err)
	}

	cfg := prow.ScannerConfig{
		ProwJobURL:     prowJobURL,
		FileNameFilter: []string{junitFilenameRegex},
	}

	scanner, err := prow.NewArtifactScanner(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize ArtifactScanner: %+v", err)
	}

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 10*time.Minute, true, func(context.Context) (done bool, err error) {
		if err := scanner.Run(); err != nil {
			klog.Errorf("Failed to scan artifacts from the Prow job due to the error: %+v...Retrying", err)
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		logger.Error().Err(err).Msgf("Timed out while scanning artifacts for Prow job %s: %+v. Will Stop processing this comment", prowJobURL, err)
		return err
	}

	overallJUnitSuites, err := getTestSuitesFromXMLFile(scanner, logger, junitFilename)
	// make sure that the Prow job didn't fail while creating the cluster
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("couldn't find the %s file", junitFilename)) {
		return fmt.Errorf("failed to get JUnitTestSuites from the file %s: %+v", junitFilename, err)
	}

	failedTCReport := setHeaderString(logger, overallJUnitSuites)
	failedTCReport.extractFailedTestCases(logger, overallJUnitSuites)

	failedTCReport.updateCommentWithFailedTestCasesReport(ctx, logger, client, event, body)

	return nil
}

// extractProwJobURLFromCommentBody extracts the
// Prow job's URL from the given PR comment's body
func extractProwJobURLFromCommentBody(logger zerolog.Logger, commentBody string) (string, error) {
	r, _ := regexp.Compile(regexToFetchProwURL)
	sliceOfMatchingString := r.FindStringSubmatch(commentBody)
	if sliceOfMatchingString == nil {
		return "", fmt.Errorf("regex string %s found no matches for the comment body: %s", regexToFetchProwURL, commentBody)
	}
	prowJobURL := sliceOfMatchingString[1]
	logger.Debug().Msgf("Prow Job's URL: %s", prowJobURL)

	return prowJobURL, nil
}

// getTestSuitesFromXMLFile returns all the JUnitTestSuites
// present within a file with the given name
func getTestSuitesFromXMLFile(scanner *prow.ArtifactScanner, logger zerolog.Logger, filename string) (*reporters.JUnitTestSuites, error) {
	overallJUnitSuites := &reporters.JUnitTestSuites{}

	for _, artifactsFilenameMap := range scanner.ArtifactStepMap {
		for artifactFilename, artifact := range artifactsFilenameMap {
			if string(artifactFilename) == filename {
				if err := xml.Unmarshal([]byte(artifact.Content), overallJUnitSuites); err != nil {
					logger.Error().Err(err).Msg("cannot decode JUnit suite into xml")
					return &reporters.JUnitTestSuites{}, err
				}
				return overallJUnitSuites, nil
			}
		}
	}

	return &reporters.JUnitTestSuites{}, fmt.Errorf("couldn't find the %s file", filename)
}

// setHeaderString initialises struct FailedTestCasesReport's
// 'headerString' field based on phase at which Prow job failed
func setHeaderString(logger zerolog.Logger, overallJUnitSuites *reporters.JUnitTestSuites) *FailedTestCasesReport {
	failedTCReport := FailedTestCasesReport{}

	if len(overallJUnitSuites.TestSuites) == 0 {
		logger.Debug().Msg("The given Prow job failed while creating the cluster")
		failedTCReport.headerString = ":rotating_light: **Error occurred while creating the cluster, please check the Prow's build logs.**\n"
	} else if len(overallJUnitSuites.TestSuites) == 1 && overallJUnitSuites.TestSuites[0].Name == openshiftCITestSuiteName {
		logger.Debug().Msg("The given Prow job failed during bootstrapping the cluster")
		failedTCReport.hasBootstrapFailure = true
		failedTCReport.headerString = ":rotating_light: **Error occurred during the cluster's Bootstrapping phase, list of failed Spec(s)**: \n"
	} else {
		logger.Debug().Msg("The given Prow job failed while running the E2E tests")
		failedTCReport.headerString = ":rotating_light: **Error occurred while running the E2E tests, list of failed Spec(s)**: \n"
	}

	return &failedTCReport
}

// extractFailedTestCases initialises the FailedTestCasesReport struct's
// 'failedTestCaseNames' field with the names of failed test cases
// within the given JUnitTestSuites. It does nothing, if the given
// JUnitTestSuites is nil.
func (failedTCReport *FailedTestCasesReport) extractFailedTestCases(logger zerolog.Logger, overallJUnitSuites *reporters.JUnitTestSuites) {
	if len(overallJUnitSuites.TestSuites) == 0 {
		return
	}

	for _, testSuite := range overallJUnitSuites.TestSuites {
		if failedTCReport.hasBootstrapFailure || (testSuite.Name == e2eTestSuiteName && (testSuite.Failures > 0 || testSuite.Errors > 0)) {
			for _, tc := range testSuite.TestCases {
				if tc.Failure != nil || tc.Error != nil {
					logger.Debug().Msgf("Found a Test Case (suiteName/testCaseName): %s/%s, that didn't pass", testSuite.Name, tc.Name)
					tcMessage := ""
					if failedTCReport.hasBootstrapFailure {
						systemErrString := strings.Split(tc.SystemErr, "\n")
						tcMessage = strings.Join(systemErrString[len(systemErrString)-16:], "\n")
					} else if (tc.Failure != nil) {
						tcMessage = tc.Failure.Message
					} else {
						tcMessage = tc.Error.Message
					}
					testCaseEntry := ":arrow_right: " + "[**`" + tc.Status + "`**] " + tc.Name + "\n```\n" + tcMessage + "\n```"
					failedTCReport.failedTestCaseNames = append(failedTCReport.failedTestCaseNames, testCaseEntry)
				}
			}
		}
	}
}

// updateCommentWithFailedTestCasesReport updates the
// PR comment's body with the names of failed test cases
func (failedTCReport *FailedTestCasesReport) updateCommentWithFailedTestCasesReport(ctx context.Context, logger zerolog.Logger, client *github.Client, event github.IssueCommentEvent, commentBody string) {
	repoOwner := event.GetRepo().GetOwner().GetLogin()
	repoName := event.GetRepo().GetName()
	commentAuthor := event.GetComment().GetUser().GetLogin()
	commentID := event.GetComment().GetID()

	logger.Debug().Msgf("Updating comment with ID:%d by %s", commentID, commentAuthor)

	msg := failedTCReport.headerString

	if failedTCReport.failedTestCaseNames != nil && len(failedTCReport.failedTestCaseNames) > 0 {
		for _, failedTCName := range failedTCReport.failedTestCaseNames {
			msg = msg + fmt.Sprintf("\n* %s\n", failedTCName)
		}
	}
	msg = msg + "\n-------------------------------\n\n" + commentBody

	prComment := github.IssueComment{
		Body: &msg,
	}

	err := wait.PollUntilContextTimeout(context.Background(), 3*time.Second, 10*time.Minute, true, func(context.Context) (done bool, err error) {
		if _, _, err := client.Issues.EditComment(ctx, repoOwner, repoName, commentID, &prComment); err != nil {
			logger.Error().Err(err).Msgf("Failed to edit the comment...Retrying")
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		logger.Error().Err(err).Msgf("Failed to edit comment (ID: %v) due to the error: %+v. Will Stop processing this comment", commentID, err)
	}

	logger.Debug().Msgf("Successfully updated comment (with ID:%d) with the names of failed test cases", commentID)
}
