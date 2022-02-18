// Copyright 2022 Palantir Technologies, Inc.
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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/rs/zerolog"
)

func TestClientLogging(t *testing.T) {
	t.Run("requestBody", func(t *testing.T) {
		req, out := newLoggingRequest("GET", "https://test.domain/path", []byte("The request"))
		rt := newStaticRoundTripper(200, []byte("The response"))

		logMiddleware := ClientLogging(zerolog.InfoLevel, LogRequestBody(".*"))
		rt = logMiddleware(rt)

		_, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("unexpected error making request: %v", err)
		}

		assertLogFields(t, out.Bytes(), map[string]interface{}{
			"method":       "GET",
			"status":       float64(200),
			"request_body": "The request",
		})
	})

	t.Run("requestBodyNoMatch", func(t *testing.T) {
		req, out := newLoggingRequest("GET", "https://test.domain/path", []byte("The request"))
		rt := newStaticRoundTripper(200, []byte("The response"))

		logMiddleware := ClientLogging(zerolog.InfoLevel, LogRequestBody("^/log-me$"))
		rt = logMiddleware(rt)

		_, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("unexpected error making request: %v", err)
		}

		assertLogFields(t, out.Bytes(), map[string]interface{}{
			"method":       "GET",
			"status":       float64(200),
			"request_body": missingField,
		})
	})

	t.Run("requestBodyNoBody", func(t *testing.T) {
		req, out := newLoggingRequest("GET", "https://test.domain/path", nil)
		rt := newStaticRoundTripper(200, []byte("The response"))

		logMiddleware := ClientLogging(zerolog.InfoLevel, LogRequestBody(".*"))
		rt = logMiddleware(rt)

		_, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("unexpected error making request: %v", err)
		}

		assertLogFields(t, out.Bytes(), map[string]interface{}{
			"method":       "GET",
			"status":       float64(200),
			"request_body": "",
		})
	})

	t.Run("responseBody", func(t *testing.T) {
		req, out := newLoggingRequest("GET", "https://test.domain/path", []byte("The request"))
		rt := newStaticRoundTripper(200, []byte("The response"))

		logMiddleware := ClientLogging(zerolog.InfoLevel, LogResponseBody(".*"))
		rt = logMiddleware(rt)

		_, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("unexpected error making request: %v", err)
		}

		assertLogFields(t, out.Bytes(), map[string]interface{}{
			"method":        "GET",
			"status":        float64(200),
			"response_body": "The response",
		})
	})

	t.Run("responseBodyNoMatch", func(t *testing.T) {
		req, out := newLoggingRequest("GET", "https://test.domain/path", []byte("The request"))
		rt := newStaticRoundTripper(200, []byte("The response"))

		logMiddleware := ClientLogging(zerolog.InfoLevel, LogResponseBody("^/log-me$"))
		rt = logMiddleware(rt)

		_, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("unexpected error making request: %v", err)
		}

		assertLogFields(t, out.Bytes(), map[string]interface{}{
			"method":        "GET",
			"status":        float64(200),
			"response_body": missingField,
		})
	})
}

func newLoggingRequest(method, url string, body []byte) (*http.Request, *bytes.Buffer) {
	var out bytes.Buffer
	logger := zerolog.New(&out)

	ctx := logger.WithContext(context.Background())

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	r, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		panic(fmt.Errorf("failed to create request: %w", err))
	}

	return r, &out
}

func newStaticRoundTripper(status int, body []byte) http.RoundTripper {
	return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			_, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}

		res := httptest.NewRecorder()
		res.WriteHeader(status)
		_, _ = res.Write(body)
		return res.Result(), nil
	})
}

var missingField struct{}

func assertLogFields(t *testing.T, out []byte, expected map[string]interface{}) {
	t.Logf("log output: %s", out)

	var actual map[string]interface{}
	if err := json.Unmarshal(out, &actual); err != nil {
		t.Fatalf("unexpected error unmarshalling log fields: %v", err)
	}

	for k, v := range expected {
		actualV, exists := actual[k]
		if v == missingField {
			if exists {
				t.Errorf("key %q should not exist in output", k)
			}
			continue
		}
		if !exists {
			t.Errorf("expected key %q does not exist", k)
			continue
		}

		if !reflect.DeepEqual(v, actualV) {
			t.Errorf("incorrect value for key %q\nexpected: %v (%T)\n  actual: %v (%T)", k,
				v, v,
				actualV, actualV,
			)
			continue
		}
	}
}
