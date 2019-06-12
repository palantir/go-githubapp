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

package githubapp

import (
	"context"
	"net/http"

	"github.com/google/go-github/github"
	"github.com/rs/zerolog"
)

const (
	DefaultWebhookRoute string = "/api/github/hook"
)

type EventHandler interface {
	// Handles returns a list of GitHub events that this handler handles
	// See https://developer.github.com/v3/activity/events/types/
	Handles() []string

	// Handle processes the GitHub event "eventType" with the given delivery ID
	// and payload. The EventDispatcher guarantees that the Handle method will
	// only be called for the events returned by Handles().
	//
	// If Handle returns an error, processing stops and the error is passed
	// directly to the configured error handler.
	//
	// Handle can optionally return a webhook response body and HTTP status to return to the client. Set to nil and zero, respectively, to use defaults (nothing and 200 OK).
	Handle(ctx context.Context, eventType, deliveryID string, payload []byte, w http.ResponseWriter) (status int, respbody []byte, err error)
}

type ErrorHandler func(http.ResponseWriter, *http.Request, error)

type eventDispatcher struct {
	handlerMap map[string]EventHandler
	secret     string
	onError    ErrorHandler
}

// NewDefaultEventDispatcher is a convenience method to create an
// EventDispatcher from configuration using the default error handler.
func NewDefaultEventDispatcher(c Config, handlers ...EventHandler) http.Handler {
	return NewEventDispatcher(handlers, c.App.WebhookSecret, nil)
}

// NewEventDispatcher creates an http.Handler that dispatches GitHub webhook
// requests to the appropriate event handlers. It validates payload integrity
// using the given secret value.
//
// If an error occurs during handling, the error handler is called with the
// error and should write an appropriate response. If the error handler is nil,
// a default handler is used.
func NewEventDispatcher(handlers []EventHandler, secret string, onError ErrorHandler) http.Handler {
	handlerMap := make(map[string]EventHandler)

	// Iterate in reverse so the first entries in the slice have priority
	for i := len(handlers) - 1; i >= 0; i-- {
		for _, event := range handlers[i].Handles() {
			handlerMap[event] = handlers[i]
		}
	}

	if onError == nil {
		onError = DefaultErrorHandler
	}

	return &eventDispatcher{
		handlerMap: handlerMap,
		secret:     secret,
		onError:    onError,
	}
}

// ServeHTTP to implement http.Handler
func (d *eventDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		// ACK payload that was received but won't be processed
		w.WriteHeader(http.StatusAccepted)
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	logger := zerolog.Ctx(ctx).With().
		Str(LogKeyEventType, eventType).
		Str(LogKeyDeliveryID, deliveryID).
		Logger()

	// update context and request to contain new log fields
	ctx = logger.WithContext(ctx)
	r = r.WithContext(ctx)

	payloadBytes, err := github.ValidatePayload(r, []byte(d.secret))
	if err != nil {
		// if payload fails validation, do not run error handler and return 400 Bad Request
		logger.Error().Err(err).Msg("invalid webhook or bad signature")
		http.Error(w, "invalid webhook or bad signature", http.StatusBadRequest)
		return
	}

	logger.Info().Msgf("Received webhook event")
	handler, ok := d.handlerMap[eventType]

	switch {
	case ok:
		status, respbody, err := handler.Handle(ctx, eventType, deliveryID, payloadBytes, w)
		if err != nil {
			// pass error directly so handler can inspect types if needed
			d.onError(w, r, err)
			return
		}
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if len(respbody) != 0 {
			if n, err := w.Write(respbody); n != len(respbody) || err != nil {
				logger.Info().Err(err).Msg("error writing response or short write")
			}
		}
	case eventType == "ping":
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// DefaultErrorHandler logs errors and responds with a 500 status code.
func DefaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	logger := zerolog.Ctx(r.Context())
	logger.Error().Err(err).Msg("Unexpected error handling webhook request")

	msg := http.StatusText(http.StatusInternalServerError)
	http.Error(w, msg, http.StatusInternalServerError)
}
