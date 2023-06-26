// Copyright 2023 Palantir Technologies, Inc.
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

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
)

// NewCachingInstallationsService returns an InstallationsService that always queries GitHub. It should be created with
// a client that authenticates as the target.
// It uses an LRU cache of the provided capacity to store app installation info for repositories and owners and returns
// cached installation info when a cache hit exists.
//
// This should be used in cases where installation info needs to be queried multiple times across a short timespan as
// the installation ID can change if an administrator uninstalls then reinstalls the app, in which case a cache hit
// would return the wrong installation ID. Use with caution for long-lived usecases.
func NewCachingInstallationsService(delegate InstallationsService, capacity int) (InstallationsService, error) {
	cache, err := lru.New(capacity)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create cache")
	}

	return &cachingInstallationsService{
		cache:    cache,
		delegate: delegate,
	}, nil
}

type cachingInstallationsService struct {
	cache    *lru.Cache
	delegate InstallationsService
}

func (c *cachingInstallationsService) ListAll(ctx context.Context) ([]Installation, error) {
	// ListAll is not cached due to the higher probability of installation IDs changing when listing all installations
	// across all organizations that the app is installed to
	return c.delegate.ListAll(ctx)
}

func (c *cachingInstallationsService) GetByOwner(ctx context.Context, owner string) (Installation, error) {
	// if installation is in cache, return it
	val, ok := c.cache.Get(owner)
	if ok {
		if install, ok := val.(Installation); ok {
			return install, nil
		}
	}

	// otherwise, get installation info, save to cache, and return
	install, err := c.delegate.GetByOwner(ctx, owner)
	if err != nil {
		return Installation{}, err
	}
	c.cache.Add(owner, install)
	return install, nil
}

func (c *cachingInstallationsService) GetByRepository(ctx context.Context, owner, name string) (Installation, error) {
	// if installation is in cache, return it
	key := fmt.Sprintf("%s/%s", owner, name)
	val, ok := c.cache.Get(key)
	if ok {
		if install, ok := val.(Installation); ok {
			return install, nil
		}
	}

	// otherwise, get installation info, save to cache, and return
	install, err := c.delegate.GetByRepository(ctx, owner, name)
	if err != nil {
		return Installation{}, err
	}
	c.cache.Add(key, install)
	return install, nil
}
