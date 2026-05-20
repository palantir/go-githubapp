// Copyright 2026 Palantir Technologies, Inc.
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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/shurcooL/githubv4"
)

func TestNewInstallationV4ClientSetsInstallationIDForMetrics(t *testing.T) {
	const installationID = int64(42)
	registry := metrics.NewRegistry()

	var observedInstallationID any
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			return jsonResponse(http.StatusCreated, fmt.Sprintf(`{"token":"token","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339)), nil), nil
		case "/graphql":
			observedInstallationID = r.Context().Value(installationKey)
			return jsonResponse(http.StatusOK, `{"data":{"viewer":{"login":"octocat"}}}`, rateLimitHeaders()), nil
		default:
			return nil, fmt.Errorf("unexpected request path: %s", r.URL.Path)
		}
	})

	creator := NewClientCreator(
		"https://api.github.test",
		"https://api.github.test/graphql",
		123,
		testPrivateKey(t),
		WithTransport(transport),
		WithClientMiddleware(ClientMetrics(registry)),
	)

	client, err := creator.NewInstallationV4Client(installationID)
	if err != nil {
		t.Fatalf("failed to create v4 installation client: %v", err)
	}

	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := client.Query(context.Background(), &query, nil); err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	assertField(t, "request installation ID", installationID, observedInstallationID)
	assertGaugeValue(t, registry, "github.rate.remaining[installation:42]", 4999)
	if metric := registry.Get("github.rate.remaining[installation:0]"); metric != nil {
		t.Fatalf("unexpected installation 0 rate limit metric: %#v", metric)
	}
}

func TestNewAppV4ClientSetsZeroInstallationIDForMetrics(t *testing.T) {
	registry := metrics.NewRegistry()

	var observedInstallationID any
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/graphql" {
			return nil, fmt.Errorf("unexpected request path: %s", r.URL.Path)
		}

		observedInstallationID = r.Context().Value(installationKey)
		return jsonResponse(http.StatusOK, `{"data":{"viewer":{"login":"octocat"}}}`, rateLimitHeaders()), nil
	})

	creator := NewClientCreator(
		"https://api.github.test",
		"https://api.github.test/graphql",
		123,
		testPrivateKey(t),
		WithTransport(transport),
		WithClientMiddleware(ClientMetrics(registry)),
	)

	client, err := creator.NewAppV4Client()
	if err != nil {
		t.Fatalf("failed to create v4 app client: %v", err)
	}

	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := client.Query(context.Background(), &query, nil); err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	assertField(t, "request installation ID", int64(0), observedInstallationID)
	assertGaugeValue(t, registry, "github.rate.remaining[installation:0]", 4999)
}

func testPrivateKey(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func jsonResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func rateLimitHeaders() http.Header {
	return http.Header{
		httpHeaderRateLimit:     []string{"5000"},
		httpHeaderRateRemaining: []string{"4999"},
		httpHeaderRateUsed:      []string{"1"},
		httpHeaderRateReset:     []string{"1779281124"},
	}
}

func assertGaugeValue(t *testing.T, registry metrics.Registry, name string, expected int64) {
	t.Helper()

	metric := registry.Get(name)
	if metric == nil {
		t.Fatalf("expected metric %q to exist", name)
	}

	gauge, ok := metric.(metrics.Gauge)
	if !ok {
		t.Fatalf("expected metric %q to be a gauge, but was %T", name, metric)
	}
	if actual := gauge.Value(); actual != expected {
		t.Fatalf("incorrect gauge value for %q: expected %d, got %d", name, expected, actual)
	}
}
