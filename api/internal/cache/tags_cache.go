package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openmentor-io/openmentor-api/pkg/logger"
	gocache "github.com/patrickmn/go-cache"
	"go.uber.org/zap"
)

const (
	tagsCacheKey = "tags"
	tagsCacheTTL = 24 * time.Hour
)

// TagsFetcher is a function that fetches all tags from the data source
type TagsFetcher func(ctx context.Context) (map[string]string, error)

// TagsCache manages the in-memory cache for tags
type TagsCache struct {
	cache   *gocache.Cache
	fetcher TagsFetcher
	mu      sync.RWMutex
	ready   bool
}

// NewTagsCache creates a new tags cache
func NewTagsCache(fetcher TagsFetcher) *TagsCache {
	cache := gocache.New(tagsCacheTTL, time.Hour)

	return &TagsCache{
		cache:   cache,
		fetcher: fetcher,
		ready:   false,
	}
}

// Initialize performs initial cache population (synchronous, blocks until ready)
// Should be called during application startup before accepting requests
func (tc *TagsCache) Initialize() error {
	logger.Info("Initializing tags cache...")
	_, err := tc.refresh()
	if err != nil {
		logger.Error("Failed to initialize tags cache", zap.Error(err))
		return err
	}

	tc.mu.Lock()
	tc.ready = true
	tc.mu.Unlock()

	logger.Info("Tags cache initialized successfully")
	return nil
}

// IsReady returns true if the cache has been successfully initialized
func (tc *TagsCache) IsReady() bool {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.ready
}

// Get retrieves tags from cache or fetches them if cache miss
func (tc *TagsCache) Get() (map[string]string, error) {
	if !tc.IsReady() {
		return nil, fmt.Errorf("tags cache not initialized")
	}

	// Check cache
	if data, found := tc.cache.Get(tagsCacheKey); found {
		logger.Debug("Tags cache hit")
		tags, ok := data.(map[string]string)
		if !ok {
			logger.Error("Invalid tags cache data type")
			tc.cache.Delete(tagsCacheKey)
			return nil, fmt.Errorf("invalid cache data type")
		}
		return tags, nil
	}

	logger.Info("Tags cache miss, fetching from database")

	// Cache miss, fetch and populate
	return tc.refresh()
}

// refresh fetches tags from the data source and updates the cache
func (tc *TagsCache) refresh() (map[string]string, error) {
	tags, err := tc.fetcher(context.Background())
	if err != nil {
		logger.Error("Failed to refresh tags cache", zap.Error(err))
		return nil, err
	}

	// Update cache
	tc.cache.Set(tagsCacheKey, tags, tagsCacheTTL)

	logger.Info("Tags cache refreshed", zap.Int("count", len(tags)))

	return tags, nil
}

// GetTagIDByName gets a single tag ID by name
func (tc *TagsCache) GetTagIDByName(name string) (string, error) {
	tags, err := tc.Get()
	if err != nil {
		return "", err
	}

	if id, found := tags[name]; found {
		return id, nil
	}

	return "", nil
}
