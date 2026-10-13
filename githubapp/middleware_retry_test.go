// Copyright 2024 Palantir Technologies, Inc.
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
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockRoundTripper records how many times it was called and returns the
// configured responses in order. The final response is repeated for any
// additional calls.
type mockRoundTripper struct {
	responses []*http.Response
	calls     int
}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	idx := m.calls
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.calls++
	return m.responses[idx], nil
}

func makeResponse(status int, headers map[string]string) *http.Response {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestRateLimitRetry_RetryAfterHeader(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(http.StatusForbidden, map[string]string{headerRetryAfter: "1"}),
			makeResponse(http.StatusOK, nil),
		},
	}

	mw := RateLimitRetry(5 * time.Second)
	rt := mw(mock)

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos", nil)
	start := time.Now()
	res, err := rt.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusOK)
	}
	if mock.calls != 2 {
		t.Errorf("RoundTrip calls: got %d, want 2", mock.calls)
	}
	if elapsed < time.Second {
		t.Errorf("expected at least 1s sleep, got %v", elapsed)
	}
}

func TestRateLimitRetry_ResetHeader(t *testing.T) {
	resetAt := time.Now().Add(1 * time.Second).Unix()

	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(http.StatusForbidden, map[string]string{
				httpHeaderRateReset: fmt.Sprintf("%d", resetAt),
			}),
			makeResponse(http.StatusOK, nil),
		},
	}

	mw := RateLimitRetry(5 * time.Second)
	rt := mw(mock)

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos", nil)
	start := time.Now()
	res, err := rt.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusOK)
	}
	if mock.calls != 2 {
		t.Errorf("RoundTrip calls: got %d, want 2", mock.calls)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("expected ~1s sleep, got %v", elapsed)
	}
}

func TestRateLimitRetry_WaitExceedsMaxWait(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(http.StatusForbidden, map[string]string{headerRetryAfter: "60"}),
			makeResponse(http.StatusOK, nil),
		},
	}

	mw := RateLimitRetry(5 * time.Second)
	rt := mw(mock)

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos", nil)
	res, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	if mock.calls != 1 {
		t.Errorf("RoundTrip calls: got %d, want 1 (should not retry)", mock.calls)
	}
}

func TestRateLimitRetry_NoRateLimitHeaders(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(http.StatusForbidden, nil),
		},
	}

	mw := RateLimitRetry(5 * time.Second)
	rt := mw(mock)

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos", nil)
	res, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	if mock.calls != 1 {
		t.Errorf("RoundTrip calls: got %d, want 1 (no rate limit header)", mock.calls)
	}
}

func TestRateLimitRetry_Non403PassThrough(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(http.StatusTooManyRequests, map[string]string{headerRetryAfter: "1"}),
		},
	}

	mw := RateLimitRetry(5 * time.Second)
	rt := mw(mock)

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos", nil)
	res, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusTooManyRequests)
	}
	if mock.calls != 1 {
		t.Errorf("RoundTrip calls: got %d, want 1 (non-403 not retried)", mock.calls)
	}
}

func TestRateLimitRetry_ZeroMaxWaitDisablesRetry(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(http.StatusForbidden, map[string]string{headerRetryAfter: "1"}),
			makeResponse(http.StatusOK, nil),
		},
	}

	mw := RateLimitRetry(0)
	rt := mw(mock)

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos", nil)
	res, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	if mock.calls != 1 {
		t.Errorf("RoundTrip calls: got %d, want 1 (maxWait=0 disables retry)", mock.calls)
	}
}
