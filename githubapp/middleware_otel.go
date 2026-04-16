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
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
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

	OTelMetricsKeyHandlerError  = "github.handler.errors"
	OTelMetricsKeyDroppedEvents = "github.event.dropped"
	OTelMetricsKeyEventAge      = "github.event.age"
)

// Package-level attribute keys avoid per-request allocation.
var (
	otelAttrInstallationID = attribute.Key("installation.id")
	otelAttrStatusClass    = attribute.Key("http.status_class")
	otelAttrEventType      = attribute.Key("github.event_type")
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
func OTelClientMetrics(mp metric.MeterProvider) ClientMiddleware {
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
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			installationID, _ := r.Context().Value(installationKey).(int64)

			res, tripErr := next.RoundTrip(r)

			if res != nil {
				ctx := r.Context()
				installAttr := otelAttrInstallationID.Int64(installationID)

				requests.Add(ctx, 1, metric.WithAttributes(installAttr))

				if sc := otelStatusClass(res.StatusCode); sc != "" {
					requestsStatus.Add(ctx, 1, metric.WithAttributes(
						installAttr,
						otelAttrStatusClass.String(sc),
					))
				}

				if res.Header.Get(httpcache.XFromCache) != "" {
					requestsCached.Add(ctx, 1, metric.WithAttributes(installAttr))
				}

				// Only record rate limit metrics when the primary header is present.
				if res.Header.Get(httpHeaderRateLimit) != "" {
					limit, _ := otelParseIntHeader(res.Header, httpHeaderRateLimit)
					remaining, _ := otelParseIntHeader(res.Header, httpHeaderRateRemaining)
					used, _ := otelParseIntHeader(res.Header, httpHeaderRateUsed)
					reset, _ := otelParseIntHeader(res.Header, httpHeaderRateReset)
					state.update(installationID, limit, remaining, used, reset)
				}
			}

			return res, tripErr
		})
	}
}

// OTelErrorCallback returns an ErrorCallback that logs errors and records them
// via OpenTelemetry. Pass nil to use the global MeterProvider.
func OTelErrorCallback(mp metric.MeterProvider) ErrorCallback {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(otelMeterName)

	handlerErrors, err := meter.Int64Counter(
		OTelMetricsKeyHandlerError,
		metric.WithDescription("Number of errors returned by GitHub webhook event handlers."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyHandlerError, err))
	}

	return func(w http.ResponseWriter, r *http.Request, cbErr error) {
		logger := zerolog.Ctx(r.Context())

		var ve ValidationError
		if errors.As(cbErr, &ve) {
			logger.Warn().Err(ve.Cause).Msgf("Received invalid webhook headers or payload")
			http.Error(w, "Invalid webhook headers or payload", http.StatusBadRequest)
			return
		}
		if errors.Is(cbErr, ErrCapacityExceeded) {
			logger.Warn().Msg("Dropping webhook event due to over-capacity scheduler")
			http.Error(w, "No capacity available to processes this event", http.StatusServiceUnavailable)
			return
		}

		logger.Error().Err(cbErr).Msg("Unexpected error handling webhook")
		eventType := r.Header.Get("X-Github-Event")
		handlerErrors.Add(r.Context(), 1, metric.WithAttributes(
			otelAttrEventType.String(eventType),
		))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// OTelAsyncErrorCallback returns an AsyncErrorCallback that logs errors and
// records them via OpenTelemetry. Pass nil to use the global MeterProvider.
func OTelAsyncErrorCallback(mp metric.MeterProvider) AsyncErrorCallback {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(otelMeterName)

	handlerErrors, err := meter.Int64Counter(
		OTelMetricsKeyHandlerError,
		metric.WithDescription("Number of errors returned by GitHub webhook event handlers."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyHandlerError, err))
	}

	return func(ctx context.Context, d Dispatch, cbErr error) {
		zerolog.Ctx(ctx).Error().Err(cbErr).Msg("Unexpected error handling webhook")
		handlerErrors.Add(ctx, 1, metric.WithAttributes(
			otelAttrEventType.String(d.EventType),
		))
	}
}

// WrapSchedulerWithOTel wraps a Scheduler to record dropped events and event
// age via OpenTelemetry. Pass nil to use the global MeterProvider.
//
// Queue depth and active worker counts are internal to the wrapped scheduler
// and are not observable through this wrapper; use WithSchedulingMetrics for
// those metrics.
func WrapSchedulerWithOTel(inner Scheduler, mp metric.MeterProvider) Scheduler {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(otelMeterName)

	dropped, err := meter.Int64Counter(
		OTelMetricsKeyDroppedEvents,
		metric.WithDescription("Number of webhook events dropped because the scheduler was at capacity."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyDroppedEvents, err))
	}

	eventAge, err := meter.Int64Histogram(
		OTelMetricsKeyEventAge,
		metric.WithDescription("Time in milliseconds between a webhook event being queued and its handler beginning execution."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(fmt.Sprintf("githubapp: failed to create OTel instrument %q: %v", OTelMetricsKeyEventAge, err))
	}

	return &otelSchedulerWrapper{
		inner:    inner,
		dropped:  dropped,
		eventAge: eventAge,
	}
}

type otelSchedulerWrapper struct {
	inner    Scheduler
	dropped  metric.Int64Counter
	eventAge metric.Int64Histogram
}

func (s *otelSchedulerWrapper) Schedule(ctx context.Context, d Dispatch) error {
	enqueueTime := time.Now()

	wrapped := Dispatch{
		Handler: &otelEventHandlerWrapper{
			inner:       d.Handler,
			enqueueTime: enqueueTime,
			eventAge:    s.eventAge,
		},
		EventType:  d.EventType,
		DeliveryID: d.DeliveryID,
		Payload:    d.Payload,
	}

	schedErr := s.inner.Schedule(ctx, wrapped)
	if schedErr != nil && errors.Is(schedErr, ErrCapacityExceeded) {
		s.dropped.Add(ctx, 1, metric.WithAttributes(
			otelAttrEventType.String(d.EventType),
		))
	}
	return schedErr
}

type otelEventHandlerWrapper struct {
	inner       EventHandler
	enqueueTime time.Time
	eventAge    metric.Int64Histogram
}

func (h *otelEventHandlerWrapper) Handles() []string {
	return h.inner.Handles()
}

func (h *otelEventHandlerWrapper) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	age := time.Since(h.enqueueTime).Milliseconds()
	h.eventAge.Record(ctx, age, metric.WithAttributes(
		otelAttrEventType.String(eventType),
	))
	return h.inner.Handle(ctx, eventType, deliveryID, payload)
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
