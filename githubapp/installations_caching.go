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
	key := fmt.Sprintf(owner, "/", name)
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
