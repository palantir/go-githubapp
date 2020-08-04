// Copyright 2020 Palantir Technologies, Inc.
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
	"testing"
	"time"

	"github.com/pkg/errors"
)

type AsyncHandler struct {
	Block  chan struct{}
	Called chan bool
	Error  error
}

func (h *AsyncHandler) Handles() []string { return []string{"ping"} }

func (h *AsyncHandler) Handle(ctx context.Context, eventType, id string, payload []byte) error {
	if h.Block != nil {
		<-h.Block
	}
	h.Called <- true
	return h.Error
}

func TestAsyncScheduler(t *testing.T) {
	const timeout = 100 * time.Millisecond

	t.Run("callsHandler", func(t *testing.T) {
		s := AsyncScheduler()
		h := AsyncHandler{Called: make(chan bool, 1)}

		if err := s.Schedule(context.Background(), Dispatch{
			Handler: &h,
		}); err != nil {
			t.Fatalf("unexpected error scheduling dispatch: %v", err)
		}

		called := false
		select {
		case called = <-h.Called:
		case <-time.After(timeout):
		}

		if !called {
			t.Fatalf("handler was not called after %v", timeout)
		}
	})

	t.Run("errorCallback", func(t *testing.T) {
		errc := make(chan error, 1)
		cb := func(ctx context.Context, d Dispatch, err error) {
			errc <- err
		}

		s := AsyncScheduler(WithAsyncErrorCallback(cb))
		h := AsyncHandler{Called: make(chan bool, 1), Error: errors.New("handler error")}

		if err := s.Schedule(context.Background(), Dispatch{
			Handler: &h,
		}); err != nil {
			t.Fatalf("unexpected error scheduling dispatch: %v", err)
		}

		var herr error
		select {
		case herr = <-errc:
		case <-time.After(timeout):
		}

		if herr == nil {
			t.Fatalf("handler did not report an error after %v", timeout)
		}
	})
}

func TestQueueAsyncScheduler(t *testing.T) {
	const timeout = 100 * time.Millisecond

	t.Run("callsHandler", func(t *testing.T) {
		s := QueueAsyncScheduler(1, 1)
		h := AsyncHandler{Called: make(chan bool, 1)}

		if err := s.Schedule(context.Background(), Dispatch{
			Handler: &h,
		}); err != nil {
			t.Fatalf("unexpected error scheduling dispatch: %v", err)
		}

		called := false
		select {
		case called = <-h.Called:
		case <-time.After(timeout):
		}

		if !called {
			t.Fatalf("handler was not called after %v", timeout)
		}
	})

	t.Run("errorCallback", func(t *testing.T) {
		errc := make(chan error, 1)
		cb := func(ctx context.Context, d Dispatch, err error) {
			errc <- err
		}

		s := QueueAsyncScheduler(1, 1, WithAsyncErrorCallback(cb))
		h := AsyncHandler{Called: make(chan bool, 1), Error: errors.New("handler error")}

		if err := s.Schedule(context.Background(), Dispatch{
			Handler: &h,
		}); err != nil {
			t.Fatalf("unexpected error scheduling dispatch: %v", err)
		}

		var herr error
		select {
		case herr = <-errc:
		case <-time.After(timeout):
		}

		if herr == nil {
			t.Fatalf("handler did not report an error after %v", timeout)
		}
	})

	t.Run("rejectEventsWhenFull", func(t *testing.T) {
		s := QueueAsyncScheduler(1, 1)
		h := AsyncHandler{Block: make(chan struct{}), Called: make(chan bool, 1)}
		ctx := context.Background()
		d := Dispatch{
			Handler: &h,
		}

		if err := s.Schedule(ctx, d); err != nil {
			t.Fatalf("unexpected error scheduling first dispatch: %v", err)
		}
		if err := s.Schedule(ctx, d); err != ErrCapacityExceeded {
			t.Fatalf("expected ErrCapacityExceeded, but got: %v", err)
		}
	})
}
