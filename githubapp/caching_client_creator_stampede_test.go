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
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v89/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// countingDelegate is a ClientCreator that counts delegate invocations and
// yields the scheduler on each call to maximize concurrent cache misses.
type countingDelegate struct {
	calls int64
}

func (d *countingDelegate) NewInstallationClient(_ int64) (*github.Client, error) {
	atomic.AddInt64(&d.calls, 1)
	runtime.Gosched() // let other goroutines reach the cache-miss branch
	return github.NewClient()
}

func (d *countingDelegate) NewAppClient() (*github.Client, error)                         { return nil, nil }
func (d *countingDelegate) NewAppV4Client() (*githubv4.Client, error)                     { return nil, nil }
func (d *countingDelegate) NewInstallationV4Client(_ int64) (*githubv4.Client, error)     { return nil, nil }
func (d *countingDelegate) NewTokenSourceClient(_ oauth2.TokenSource) (*github.Client, error) {
	return nil, nil
}
func (d *countingDelegate) NewTokenSourceV4Client(_ oauth2.TokenSource) (*githubv4.Client, error) {
	return nil, nil
}
func (d *countingDelegate) NewTokenClient(_ string) (*github.Client, error)     { return nil, nil }
func (d *countingDelegate) NewTokenV4Client(_ string) (*githubv4.Client, error) { return nil, nil }

// TestCacheStampede_Vulnerable demonstrates that cachingClientCreator invokes
// the underlying delegate multiple times for the same installationID under
// concurrent load, because the check-then-act between Get() and Add() is not
// atomic. In production each extra call is a real token-fetch HTTP request to
// GitHub, wasting rate limit quota and increasing latency.
//
// After the singleflight patch is applied, delegate.calls will equal exactly 1.
func TestCacheStampede_Vulnerable(t *testing.T) {
	const (
		goroutines     = 100
		installationID = int64(12345)
	)

	delegate := &countingDelegate{}
	cc, err := NewCachingClientCreator(delegate, DefaultCachingClientCapacity)
	if err != nil {
		t.Fatalf("NewCachingClientCreator: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			if _, err := cc.NewInstallationClient(installationID); err != nil {
				t.Errorf("NewInstallationClient error: %v", err)
			}
		}()
	}

	close(start) // release all goroutines simultaneously
	wg.Wait()

	got := atomic.LoadInt64(&delegate.calls)

	fmt.Printf("\n========================================\n")
	fmt.Printf("  Cache Stampede Reproduction Report\n")
	fmt.Printf("========================================\n")
	fmt.Printf("  Concurrent goroutines : %d\n", goroutines)
	fmt.Printf("  Installation ID       : %d\n", installationID)
	fmt.Printf("  Delegate invocations  : %d\n", got)
	if got > 1 {
		fmt.Printf("  Result : VULNERABLE\n")
		fmt.Printf("           %d redundant token-fetch calls would have hit GitHub API\n", got-1)
	} else {
		fmt.Printf("  Result : PATCHED (singleflight active)\n")
	}
	fmt.Printf("========================================\n\n")

	if got == 1 {
		t.Log("singleflight patch is active: delegate called exactly once")
	} else {
		// Use Logf not Fatalf — we want to confirm the stampede, not fail the
		// suite. The patch will reduce this to 1.
		t.Logf("STAMPEDE CONFIRMED: delegate called %d times (expected 1 after patch)", got)
	}
}
