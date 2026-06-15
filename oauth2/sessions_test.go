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

package oauth2

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs"
	"github.com/alexedwards/scs/stores/memstore"
)

// An empty state means no OAuth flow was initiated.
// VerifyState must reject it before comparing state values,
// since the comparison treats two empty strings as a match.
func TestSessionStateStore_VerifyState_RejectsEmptyState(t *testing.T) {
	store := &SessionStateStore{Sessions: scs.NewManager(memstore.New(0))}

	r := httptest.NewRequest(http.MethodGet, "/callback?state=", nil)

	ok, err := store.VerifyState(r, r.FormValue("state"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("VerifyState accepted an empty/unstored state")
	}
}
