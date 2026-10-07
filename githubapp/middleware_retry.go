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
	"net/http"
	"strconv"
	"time"
)

const (
	headerRetryAfter = "Retry-After"
)

// RateLimitRetry returns a ClientMiddleware that transparently retries a
// request exactly once when GitHub responds with 403 and a rate-limit wait
// header (Retry-After or X-RateLimit-Reset). If the required wait exceeds
// maxWait the original 403 response is returned without retrying.
func RateLimitRetry(maxWait time.Duration) ClientMiddleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			res, err := next.RoundTrip(r)
			if err != nil || res == nil || res.StatusCode != http.StatusForbidden {
				return res, err
			}

			wait := rateLimitWait(res)
			if wait <= 0 || wait > maxWait {
				return res, err
			}

			// drain and close the 403 body before retrying
			closeBody(res.Body)

			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return res, r.Context().Err()
			case <-timer.C:
			}

			return next.RoundTrip(r)
		})
	}
}

// WithRateLimitRetry adds rate-limit retry middleware to all clients created
// by the ClientCreator. See RateLimitRetry for retry semantics.
func WithRateLimitRetry(maxWait time.Duration) ClientOption {
	return WithClientMiddleware(RateLimitRetry(maxWait))
}

// rateLimitWait returns the duration to wait based on GitHub rate-limit
// response headers. It checks Retry-After first, then X-RateLimit-Reset.
// Returns 0 if neither header is present or parseable.
func rateLimitWait(res *http.Response) time.Duration {
	if v := res.Header.Get(headerRetryAfter); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}

	if v := res.Header.Get(httpHeaderRateReset); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			wait := time.Until(time.Unix(epoch, 0))
			if wait > 0 {
				return wait
			}
		}
	}

	return 0
}
