// Copyright 2019 Palantir Technologies, Inc.
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
	"crypto/hmac"
	"crypto/sha1"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rs/zerolog"
)

const (
	testHookSecret = "secrethooksecret"
)

func TestEventDispatcher(t *testing.T) {
	tests := map[string]struct {
		Handler TestEventHandler
		Options []DispatcherOption
		Event   string
		Invalid bool

		ResponseCode int
		ResponseBody string
		CallCount    int
	}{
		"unhandledEvent": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
			},
			Event:        "issue_comment",
			ResponseCode: 202,
		},
		"pingIsHandled": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
			},
			Event:        "ping",
			ResponseCode: 200,
		},
		"callsRegisteredHandler": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
			},
			Event:        "pull_request",
			ResponseCode: 200,
			CallCount:    1,
		},
		"defaultErrorHandlerReturns500OnError": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
				Fn: func(ctx context.Context, eventType, deliveryID string, payload []byte) error {
					return errors.New("handler failure")
				},
			},
			Event:        "pull_request",
			ResponseCode: 500,
			ResponseBody: "Internal Server Error\n",
			CallCount:    1,
		},
		"defaultErrorHandlerReturns400OnInvalid": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
			},
			Event:        "pull_request",
			Invalid:      true,
			ResponseCode: 400,
			ResponseBody: "Invalid webhook headers or payload\n",
		},
		"callsCustomErrorCallback": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
				Fn: func(ctx context.Context, eventType, deliveryID string, payload []byte) error {
					return errors.New("handler failure")
				},
			},
			Options: []DispatcherOption{
				WithErrorCallback(func(w http.ResponseWriter, r *http.Request, err error) {
					http.Error(w, "Already processed this pull request!", 409)
				}),
			},
			Event:        "pull_request",
			ResponseCode: 409,
			ResponseBody: "Already processed this pull request!\n",
			CallCount:    1,
		},
		"callsCustomResponseCallbackHandled": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
			},
			Options: []DispatcherOption{
				WithResponseCallback(func(w http.ResponseWriter, r *http.Request, event string, handled bool) {
					if handled {
						http.Error(w, fmt.Sprintf("Created an entry for the %s event!", event), 201)
					} else {
						http.Error(w, fmt.Sprintf("No handler for the %s event!", event), 404)
					}
				}),
			},
			Event:        "pull_request",
			ResponseCode: 201,
			ResponseBody: "Created an entry for the pull_request event!\n",
			CallCount:    1,
		},
		"callsCustomResponseCallbackNotHandled": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
			},
			Options: []DispatcherOption{
				WithResponseCallback(func(w http.ResponseWriter, r *http.Request, event string, handled bool) {
					if handled {
						http.Error(w, fmt.Sprintf("Created an entry for the %s event!", event), 201)
					} else {
						http.Error(w, fmt.Sprintf("No handler for the %s event!", event), 404)
					}
				}),
			},
			Event:        "issue_comment",
			ResponseCode: 404,
			ResponseBody: "No handler for the issue_comment event!\n",
		},
		"callsHandlerResponder": {
			Handler: TestEventHandler{
				Types: []string{"pull_request"},
				Fn: func(ctx context.Context, eventType, deliveryID string, payload []byte) error {
					SetResponder(ctx, func(w http.ResponseWriter, r *http.Request) {
						http.Error(w, "I'm a teapot!", 418)
					})
					return nil
				},
			},
			Event:        "pull_request",
			ResponseCode: 418,
			ResponseBody: "I'm a teapot!\n",
			CallCount:    1,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h := test.Handler
			d := NewEventDispatcher([]EventHandler{&h}, testHookSecret, test.Options...)

			req := newHookRequest(test.Event, name, !test.Invalid)
			res := httptest.NewRecorder()
			d.ServeHTTP(res, req)

			if test.ResponseCode != res.Code {
				t.Errorf("incorrect response code: expected %d, actual %d", test.ResponseCode, res.Code)
			}
			if test.ResponseBody != res.Body.String() {
				t.Errorf("incorrect response body:\nexpected: %q\n  actual: %q", test.ResponseBody, res.Body.String())
			}
			if test.CallCount != h.Count {
				t.Errorf("incorrect call count: expected %d, actual %d", test.CallCount, h.Count)
			}
		})
	}
}

func TestSetAndGetResponder(t *testing.T) {
	t.Run("setPanicsOutsideOfDispatcher", func(t *testing.T) {
		defer func() {
			if err := recover(); err == nil {
				t.Errorf("expected SetResponder to panic, but it did not!")
			}
		}()

		ctx := context.Background()
		SetResponder(ctx, func(w http.ResponseWriter, r *http.Request) {})
	})
}

func newHookRequest(eventType, id string, signed bool) *http.Request {
	body := []byte(fmt.Sprintf(`{"type":"%s"}`, eventType))

	req := httptest.NewRequest(http.MethodPost, "/api/github/hook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Github-Event", eventType)
	req.Header.Set("X-Github-Delivery", id)

	if signed {
		mac := hmac.New(sha1.New, []byte(testHookSecret))
		mac.Write(body)
		req.Header.Set("X-Hub-Signature", fmt.Sprintf("sha1=%x", mac.Sum(nil)))
	}

	log := zerolog.New(os.Stdout)
	req = req.WithContext(log.WithContext(req.Context()))

	return req
}

type TestEventHandler struct {
	Types []string
	Fn    func(context.Context, string, string, []byte) error
	Count int
}

func (h *TestEventHandler) Handles() []string {
	return h.Types
}

func (h *TestEventHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	h.Count++
	if h.Fn != nil {
		return h.Fn(ctx, eventType, deliveryID, payload)
	}
	return nil
}
