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
	"fmt"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/rcrowley/go-metrics"
	"github.com/rs/zerolog"
)

const (
	MetricsKeyQueueLength   = "github.event.queued"
	MetricsKeyActiveWorkers = "github.event.workers"
)

var (
	ErrCapacityExceeded = errors.New("scheduler: capacity exceeded")
)

// Dispatch is a webhook payload and the handler that handles it.
type Dispatch struct {
	Handler EventHandler
	Ctx     context.Context

	EventType  string
	DeliveryID string
	Payload    []byte
}

// Execute calls the Dispatch's handler with the stored arguments.
func (d Dispatch) Execute() error {
	return d.Handler.Handle(d.Ctx, d.EventType, d.DeliveryID, d.Payload)
}

// AsyncErrorCallback is called by an asynchronous scheduler when an event
// handler returns an error. The error from the handler is passed directly as
// the final argument.
type AsyncErrorCallback func(ctx context.Context, err error)

// DefaultAsyncErrorCallback logs errors.
func DefaultAsyncErrorCallback(ctx context.Context, err error) {
	zerolog.Ctx(ctx).Error().Err(err).Msg("Unexpected error handling webhook")
}

// ContextDeriver creates a new independent context from a request's context.
// The new context must be based on context.Background(), not the input.
type ContextDeriver func(context.Context) context.Context

// DefaultContextDeriver copies the logger from the request's context to a new
// context.
func DefaultContextDeriver(ctx context.Context) context.Context {
	newCtx := context.Background()

	// this value is always unused by async schedulers, but is set for
	// compatibility with existing handlers that call SetResponder
	newCtx = InitializeResponder(newCtx)

	return zerolog.Ctx(ctx).WithContext(newCtx)
}

// Scheduler is a strategy for executing event handlers.
//
// The Schedule method takes a Dispatch and executes it by calling the handler
// for the payload. The execution may be asynchronous, but the scheduler must
// create a new context in this case. The dispatcher waits for Schedule to
// return before responding to GitHub, so asynchronous schedulers should only
// return errors that happen during scheduling, not during execution.
//
// Schedule may return ErrCapacityExceeded if it cannot schedule or queue new
// events at the time of the call.
type Scheduler interface {
	Schedule(d Dispatch) error
}

// SchedulerOption configures properties of a scheduler.
type SchedulerOption func(*scheduler)

// WithAsyncErrorCallback sets the error callback for an asynchronous
// scheduler. If not set, the scheduler uses DefaultAsyncErrorCallback.
func WithAsyncErrorCallback(onError AsyncErrorCallback) SchedulerOption {
	return func(s *scheduler) {
		if onError != nil {
			s.onError = onError
		}
	}
}

// WithContextDeriver sets the context deriver for an asynchronous scheduler.
// If not set, the scheduler uses DefaultContextDeriver.
func WithContextDeriver(deriver ContextDeriver) SchedulerOption {
	return func(s *scheduler) {
		if deriver != nil {
			s.deriver = deriver
		}
	}
}

// WithSchedulingMetrics enables metrics reporting for schedulers.
func WithSchedulingMetrics(r metrics.Registry) SchedulerOption {
	return func(s *scheduler) {
		metrics.NewRegisteredFunctionalGauge(MetricsKeyQueueLength, r, func() int64 {
			return int64(len(s.queue))
		})
		metrics.NewRegisteredFunctionalGauge(MetricsKeyActiveWorkers, r, func() int64 {
			return atomic.LoadInt64(&s.activeWorkers)
		})
	}
}

// core functionality and options for (async) schedulers
type scheduler struct {
	onError AsyncErrorCallback
	deriver ContextDeriver

	activeWorkers int64
	queue         chan Dispatch
}

func (s *scheduler) safeExecute(d Dispatch) {
	var err error
	defer func() {
		if r := recover(); r != nil {
			if rerr, ok := r.(error); ok {
				err = rerr
			} else {
				err = fmt.Errorf("%v", r)
			}
		}
		if err != nil && s.onError != nil {
			s.onError(d.Ctx, err)
		}
		atomic.AddInt64(&s.activeWorkers, -1)
	}()

	atomic.AddInt64(&s.activeWorkers, 1)
	if s.deriver != nil {
		d.Ctx = s.deriver(d.Ctx)
	}
	err = d.Execute()
}

// DefaultScheduler returns a scheduler that executes handlers in the go
// routine of the caller and returns any error.
func DefaultScheduler() Scheduler {
	return &defaultScheduler{}
}

type defaultScheduler struct{}

func (s *defaultScheduler) Schedule(d Dispatch) error {
	return d.Execute()
}

// AsyncScheduler returns a scheduler that executes handlers in new goroutines.
// Goroutines are not reused and there is no limit on the number created.
func AsyncScheduler(opts ...SchedulerOption) Scheduler {
	s := &asyncScheduler{
		scheduler: scheduler{
			deriver: DefaultContextDeriver,
			onError: DefaultAsyncErrorCallback,
		},
	}
	for _, opt := range opts {
		opt(&s.scheduler)
	}
	return s
}

type asyncScheduler struct {
	scheduler
}

func (s *asyncScheduler) Schedule(d Dispatch) error {
	go s.safeExecute(d)
	return nil
}

// QueueAsyncScheduler returns a scheduler that executes handlers in a fixed
// number of worker goroutines. If no workers are available, events queue until
// the queue is full.
func QueueAsyncScheduler(queueSize int, workers int, opts ...SchedulerOption) Scheduler {
	if queueSize < 0 {
		panic("NewQueueAsyncScheduler: queue size must be non-negative")
	}
	if workers < 1 {
		panic("NewQueueAsyncScheduler: worker count must be positive")
	}

	s := &queueScheduler{
		scheduler: scheduler{
			deriver: DefaultContextDeriver,
			onError: DefaultAsyncErrorCallback,
			queue:   make(chan Dispatch, queueSize),
		},
	}
	for _, opt := range opts {
		opt(&s.scheduler)
	}

	for i := 0; i < workers; i++ {
		go func() {
			for d := range s.queue {
				s.safeExecute(d)
			}
		}()
	}

	return s
}

type queueScheduler struct {
	scheduler
}

func (s *queueScheduler) Schedule(d Dispatch) error {
	select {
	case s.queue <- d:
	default:
		return ErrCapacityExceeded
	}
	return nil
}
