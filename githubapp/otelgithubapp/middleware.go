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

package otelgithubapp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/gregjones/httpcache"
	"github.com/palantir/go-githubapp/githubapp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	otelMeterName = "github.com/palantir/go-githubapp"

	OTelMetricsKeyRequests       = "github.requests"
	OTelMetricsKeyRequestsStatus = "github.requests.status"
	OTelMetricsKeyRequestsCached = "github.requests.cached"

	OTelMetricsKeyRateLimit          = "github.rate.limit"
	OTelMetricsKeyRateLimitRemaining = "github.rate.remaining"
	OTelMetricsKeyRateLimitUsed      = "github.rate.used"
	OTelMetricsKeyRateLimitReset     = "github.rate.reset"
)

// Package-level attribute keys avoid per-request allocation.
var (
	otelAttrInstallationID = attribute.Key("installation.id")
	otelAttrStatusClass    = attribute.Key("http.status_class")
)

type otelRateLimitEntry struct {
	limit, remaining, used, reset int64
}

// otelRateLimitState holds per-installation rate limit values for OTel observable gauges.
type otelRateLimitState struct {
	mu      sync.RWMutex
	entries map[int64]otelRateLimitEntry
}

func newOtelRateLimitState() *otelRateLimitState {
	return &otelRateLimitState{entries: make(map[int64]otelRateLimitEntry)}
}

func (s *otelRateLimitState) update(installationID, limit, remaining, used, reset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[installationID] = otelRateLimitEntry{
		limit:     limit,
		remaining: remaining,
		used:      used,
		reset:     reset,
	}
}

func (s *otelRateLimitState) observe(
	o metric.Observer,
	limitG, remainingG, usedG, resetG metric.Int64ObservableGauge,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, e := range s.entries {
		attrs := metric.WithAttributes(otelAttrInstallationID.Int64(id))
		o.ObserveInt64(limitG, e.limit, attrs)
		o.ObserveInt64(remainingG, e.remaining, attrs)
		o.ObserveInt64(usedG, e.used, attrs)
		o.ObserveInt64(resetG, e.reset, attrs)
	}
}

// OTelClientMetrics returns a ClientMiddleware that records GitHub API request
// metrics via OpenTelemetry. Pass nil to use the global MeterProvider.
//
// Unlike ClientMetrics, dimensions such as installation_id are expressed as
// OTel attributes rather than being encoded into metric names.
func OTelClientMetrics(mp metric.MeterProvider) githubapp.ClientMiddleware {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(otelMeterName)

	requests, err := meter.Int64Counter(
		OTelMetricsKeyRequests,
		metric.WithDescription("Total number of GitHub API requests made."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRequests, err))
	}

	requestsStatus, err := meter.Int64Counter(
		OTelMetricsKeyRequestsStatus,
		metric.WithDescription("GitHub API requests grouped by HTTP status class."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRequestsStatus, err))
	}

	requestsCached, err := meter.Int64Counter(
		OTelMetricsKeyRequestsCached,
		metric.WithDescription("GitHub API requests served from the HTTP cache."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRequestsCached, err))
	}

	state := newOtelRateLimitState()

	limitGauge, err := meter.Int64ObservableGauge(
		OTelMetricsKeyRateLimit,
		metric.WithDescription("GitHub API rate limit ceiling for the current window."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRateLimit, err))
	}

	remainingGauge, err := meter.Int64ObservableGauge(
		OTelMetricsKeyRateLimitRemaining,
		metric.WithDescription("GitHub API requests remaining in the current rate limit window."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRateLimitRemaining, err))
	}

	usedGauge, err := meter.Int64ObservableGauge(
		OTelMetricsKeyRateLimitUsed,
		metric.WithDescription("GitHub API requests used in the current rate limit window."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRateLimitUsed, err))
	}

	resetGauge, err := meter.Int64ObservableGauge(
		OTelMetricsKeyRateLimitReset,
		metric.WithDescription("Unix timestamp at which the current GitHub API rate limit window resets."),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyRateLimitReset, err))
	}

	_, err = meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			state.observe(o, limitGauge, remainingGauge, usedGauge, resetGauge)
			return nil
		},
		limitGauge, remainingGauge, usedGauge, resetGauge,
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to register OTel rate limit callback: %v", err))
	}

	return func(next http.RoundTripper) http.RoundTripper {
		return &roundTripperWrapper{
			next:           next,
			requests:       requests,
			requestsStatus: requestsStatus,
			requestsCached: requestsCached,
			state:          state,
		}
	}
}

type roundTripperWrapper struct {
	next           http.RoundTripper
	requests       metric.Int64Counter
	requestsStatus metric.Int64Counter
	requestsCached metric.Int64Counter
	state          *otelRateLimitState
}

func (rt *roundTripperWrapper) RoundTrip(r *http.Request) (*http.Response, error) {
	installationID, ok := r.Context().Value(githubapp.InstallationIDContextKey).(int64)
	if !ok {
		installationID = 0
	}

	res, tripErr := rt.next.RoundTrip(r)

	if res != nil {
		ctx := r.Context()
		installAttr := otelAttrInstallationID.Int64(installationID)

		rt.requests.Add(ctx, 1, metric.WithAttributes(installAttr))

		if sc := otelStatusClass(res.StatusCode); sc != "" {
			rt.requestsStatus.Add(ctx, 1, metric.WithAttributes(
				installAttr,
				otelAttrStatusClass.String(sc),
			))
		}

		if res.Header.Get(httpcache.XFromCache) != "" {
			rt.requestsCached.Add(ctx, 1, metric.WithAttributes(installAttr))
		}

		// Only record rate limit metrics when the primary header is present.
		if res.Header.Get("X-RateLimit-Limit") != "" {
			limit, _ := otelParseIntHeader(res.Header, "X-RateLimit-Limit")
			remaining, _ := otelParseIntHeader(res.Header, "X-RateLimit-Remaining")
			used, _ := otelParseIntHeader(res.Header, "X-RateLimit-Used")
			reset, _ := otelParseIntHeader(res.Header, "X-RateLimit-Reset")
			rt.state.update(installationID, limit, remaining, used, reset)
		}
	}

	return res, tripErr
}

func otelStatusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500 && status < 600:
		return "5xx"
	}
	return ""
}

func otelParseIntHeader(headers http.Header, header string) (int64, bool) {
	val := headers.Get(header)
	if val == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
