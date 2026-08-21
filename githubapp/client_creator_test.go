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

package githubapp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gregjones/httpcache"
)

func TestCacheDoesNotStoreServerErrors(t *testing.T) {
	tests := map[string]struct {
		AlwaysValidate       bool
		CacheControl         string
		ExpectedCacheControl string
	}{
		"useFreshCachedResponses": {
			AlwaysValidate:       false,
			CacheControl:         "public, max-age=3600, must-revalidate",
			ExpectedCacheControl: "public, max-age=3600, must-revalidate, no-store",
		},
		"alwaysValidateCachedResponses": {
			AlwaysValidate:       true,
			CacheControl:         "public, max-age=3600, must-revalidate",
			ExpectedCacheControl: "public, max-age=0, must-revalidate, no-store",
		},
		"noStoreAlreadyPresent": {
			AlwaysValidate:       false,
			CacheControl:         "public, max-age=3600, must-revalidate, no-store",
			ExpectedCacheControl: "public, max-age=3600, must-revalidate, no-store",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			const requestURL = "https://api.github.com/resource"

			memoryCache := httpcache.NewMemoryCache()
			responseDate := time.Now().UTC().Format(http.TimeFormat)
			responses := []cacheTestResponse{
				{
					Status: http.StatusInternalServerError,
					Header: http.Header{
						"Cache-Control": {test.CacheControl},
						"Date":          {responseDate},
						"Etag":          {`"server-error"`},
					},
				},
				{
					Status: http.StatusOK,
					Header: http.Header{
						"Cache-Control": {"public, max-age=3600"},
						"Date":          {responseDate},
						"Etag":          {`"success"`},
					},
				},
			}
			if test.AlwaysValidate {
				responses = append(responses, cacheTestResponse{
					Status:              http.StatusNotModified,
					ExpectedIfNoneMatch: `"success"`,
				})
			}

			calls := 0
			mockTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				response := responses[calls]
				calls++
				if actual := req.Header.Get("If-None-Match"); actual != response.ExpectedIfNoneMatch {
					t.Errorf("incorrect validator on transport call %d: expected %q, actual %q",
						calls, response.ExpectedIfNoneMatch, actual)
				}
				return newCacheTestResponse(response.Status, response.Header), nil
			})

			client := &http.Client{Transport: mockTransport}
			applyMiddleware(client, [][]ClientMiddleware{{
				cache(func() httpcache.Cache { return memoryCache }),
				cacheControl(test.AlwaysValidate),
			}})

			firstResp := assertCacheTestRequest(t, client, requestURL, http.StatusInternalServerError)
			if actual := firstResp.Header.Get("Cache-Control"); actual != test.ExpectedCacheControl {
				t.Errorf("incorrect Cache-Control header: expected %q, actual %q", test.ExpectedCacheControl, actual)
			}

			assertCacheTestRequest(t, client, requestURL, http.StatusOK)
			if expected, actual := 2, calls; actual != expected {
				t.Fatalf("incorrect underlying call count after retry: expected %d, actual %d", expected, actual)
			}

			assertCacheTestRequest(t, client, requestURL, http.StatusOK)
			if expected, actual := len(responses), calls; actual != expected {
				t.Errorf("incorrect final underlying call count: expected %d, actual %d", expected, actual)
			}
		})
	}
}

type cacheTestResponse struct {
	Status              int
	Header              http.Header
	ExpectedIfNoneMatch string
}

func newCacheTestResponse(status int, header http.Header) *http.Response {
	recorder := httptest.NewRecorder()
	for key, values := range header {
		recorder.Header()[key] = values
	}
	recorder.WriteHeader(status)
	return recorder.Result()
}

func assertCacheTestRequest(
	t *testing.T,
	client *http.Client,
	requestURL string,
	expectedStatus int,
) *http.Response {
	resp, err := client.Get(requestURL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if actual := resp.StatusCode; actual != expectedStatus {
		t.Fatalf("incorrect response status: expected %d, actual %d", expectedStatus, actual)
	}
	return resp
}
