package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	gocache "github.com/patrickmn/go-cache"
	"go.uber.org/zap"
)

const (
	mentorKeyPrefix  = "mentor:slug:"
	allMentorsKey    = "mentor:all"
	metadataKey      = "mentor:metadata"
	cacheCheckPeriod = 10 * time.Second
	maxRetries       = 3
	initialRetryWait = 2 * time.Second
)

// MentorFetcher is a function that fetches all mentors from the data source
type MentorFetcher func(ctx context.Context) ([]*models.Mentor, error)

// SingleMentorFetcher is a function that fetches a single mentor by slug
type SingleMentorFetcher func(ctx context.Context, slug string) (*models.Mentor, error)

// CacheMetadata stores cache-wide information
type CacheMetadata struct {
	LastRefreshTime time.Time
	MentorCount     int
	Version         int64
}

// MentorCache manages the in-memory cache for mentors using slug-based storage
type MentorCache struct {
	cache         *gocache.Cache
	fetcher       MentorFetcher
	singleFetcher SingleMentorFetcher
	mu            sync.RWMutex
	refreshing    bool
	ready         bool
	ttl           time.Duration
	lastRefresh   time.Time
}

// NewMentorCache creates a new mentor cache with slug-based storage
func NewMentorCache(fetcher MentorFetcher, singleFetcher SingleMentorFetcher, ttlSeconds int) *MentorCache {
	ttl := time.Duration(ttlSeconds) * time.Second
	cache := gocache.New(gocache.NoExpiration, cacheCheckPeriod)

	mc := &MentorCache{
		cache:         cache,
		fetcher:       fetcher,
		singleFetcher: singleFetcher,
		refreshing:    false,
		ready:         false,
		ttl:           ttl,
	}

	return mc
}

// Initialize performs initial cache population (synchronous, blocks until ready)
// Should be called during application startup before accepting requests
func (mc *MentorCache) Initialize() error {
	logger.Info("Initializing mentor cache...")
	startTime := time.Now()

	err := mc.refreshWithRetry()
	if err != nil {
		logger.Error("Failed to initialize mentor cache", zap.Error(err))
		return err
	}

	mc.mu.Lock()
	mc.ready = true
	mc.lastRefresh = time.Now()
	mc.mu.Unlock()

	duration := time.Since(startTime)
	logger.Info("Mentor cache initialized successfully",
		zap.Duration("duration", duration))

	// Start background refresh scheduler
	go mc.schedulePeriodicRefresh()

	return nil
}

// IsReady returns true if the cache has been successfully initialized
func (mc *MentorCache) IsReady() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.ready
}

// GetBySlug retrieves a single mentor by slug with O(1) complexity
// Returns immediately without blocking, never triggers database fetch
func (mc *MentorCache) GetBySlug(slug string) (*models.Mentor, error) {
	if !mc.IsReady() {
		return nil, fmt.Errorf("cache not initialized")
	}

	key := mentorKeyPrefix + slug

	// Simple cache lookup - no fetch on miss
	data, found := mc.cache.Get(key)
	if !found {
		metrics.CacheMisses.WithLabelValues("mentor_by_slug").Inc()
		logger.Debug("Mentor not found in cache", zap.String("slug", slug))
		return nil, fmt.Errorf("mentor not found")
	}

	metrics.CacheHits.WithLabelValues("mentor_by_slug").Inc()

	mentor, ok := data.(*models.Mentor)
	if !ok {
		logger.Error("Invalid cache data type", zap.String("slug", slug))
		mc.cache.Delete(key)
		return nil, fmt.Errorf("invalid cache data")
	}

	// Return immediately, even if data might be stale
	return mentor, nil
}

// Get retrieves all mentors from cache
// Returns immediately without blocking, never triggers database fetch
func (mc *MentorCache) Get() ([]*models.Mentor, error) {
	if !mc.IsReady() {
		return nil, fmt.Errorf("cache not initialized")
	}

	// Get slug list
	slugsData, found := mc.cache.Get(allMentorsKey)
	if !found {
		// This should rarely happen - means cache expired
		// Return empty rather than blocking
		metrics.CacheMisses.WithLabelValues("mentor_all").Inc()
		logger.Warn("All mentors list not in cache (expired), returning empty")
		return []*models.Mentor{}, nil
	}

	slugs, ok := slugsData.([]string)
	if !ok {
		logger.Error("Invalid cache data type for all mentors list")
		return []*models.Mentor{}, nil
	}

	metrics.CacheHits.WithLabelValues("mentor_all").Inc()

	// Fetch each mentor from cache
	mentors := make([]*models.Mentor, 0, len(slugs))
	for _, slug := range slugs {
		mentor, err := mc.GetBySlug(slug)
		if err != nil {
			// Skip missing mentors rather than failing
			logger.Debug("Mentor missing from cache", zap.String("slug", slug))
			continue
		}
		mentors = append(mentors, mentor)
	}

	return mentors, nil
}

// UpdateSingleMentor updates ONE mentor in cache
// Called ONLY by webhook or profile update flow
func (mc *MentorCache) UpdateSingleMentor(slug string) error {
	if !mc.IsReady() {
		return fmt.Errorf("cache not initialized")
	}

	logger.Info("Updating single mentor in cache", zap.String("slug", slug))

	// Fetch fresh data using the single mentor fetcher
	mentor, err := mc.singleFetcher(context.Background(), slug)
	if err != nil {
		logger.Error("Failed to fetch mentor",
			zap.String("slug", slug),
			zap.Error(err))
		return err
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Update the individual mentor cache entry (no expiration)
	key := mentorKeyPrefix + slug
	mc.cache.Set(key, mentor, gocache.NoExpiration)

	// Ensure slug is in the all-mentors list
	if err := mc.ensureMentorInListLocked(slug); err != nil {
		logger.Error("Failed to update all-mentors list", zap.Error(err))
		// Non-fatal - mentor is still cached
	}

	metrics.CacheSize.WithLabelValues("mentor_single_update").Inc()
	logger.Info("Single mentor updated successfully", zap.String("slug", slug))

	return nil
}

// RemoveMentor removes a mentor from cache (for deletions)
func (mc *MentorCache) RemoveMentor(slug string) error {
	if !mc.IsReady() {
		return fmt.Errorf("cache not initialized")
	}

	logger.Info("Removing mentor from cache", zap.String("slug", slug))

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Remove mentor entry
	key := mentorKeyPrefix + slug
	mc.cache.Delete(key)

	// Remove from all-mentors list
	slugsData, found := mc.cache.Get(allMentorsKey)
	if !found {
		return nil // List expired
	}

	slugs, ok := slugsData.([]string)
	if !ok {
		return fmt.Errorf("invalid all-mentors list type")
	}

	// Filter out the slug
	newSlugs := make([]string, 0, len(slugs))
	for _, s := range slugs {
		if s != slug {
			newSlugs = append(newSlugs, s)
		}
	}

	// Update list with remaining TTL
	mc.cache.Set(allMentorsKey, newSlugs, mc.ttl)

	logger.Info("Mentor removed from cache", zap.String("slug", slug))
	return nil
}

// ForceRefresh triggers a background refresh and returns immediately
func (mc *MentorCache) ForceRefresh() ([]*models.Mentor, error) {
	logger.Info("Force refresh requested, triggering background refresh")

	// Trigger background refresh (non-blocking)
	go func() {
		if err := mc.refreshInBackground(); err != nil {
			logger.Error("Background refresh failed", zap.Error(err))
		}
	}()

	// Return current cached data immediately
	return mc.Get()
}

// schedulePeriodicRefresh runs background refresh at TTL intervals
func (mc *MentorCache) schedulePeriodicRefresh() {
	ticker := time.NewTicker(mc.ttl)
	defer ticker.Stop()

	for range ticker.C {
		logger.Info("Starting scheduled cache refresh")

		if err := mc.refreshInBackground(); err != nil {
			logger.Error("Scheduled cache refresh failed", zap.Error(err))
			// Don't stop the scheduler - will retry on next tick
		}
	}
}

// refreshInBackground performs non-blocking background refresh
func (mc *MentorCache) refreshInBackground() error {
	mc.mu.Lock()

	// Check if already refreshing
	if mc.refreshing {
		mc.mu.Unlock()
		logger.Debug("Refresh already in progress, skipping")
		return nil
	}

	mc.refreshing = true
	mc.mu.Unlock()

	defer func() {
		mc.mu.Lock()
		mc.refreshing = false
		mc.mu.Unlock()
	}()

	logger.Info("Background refresh started")
	startTime := time.Now()

	// Fetch all mentors
	mentors, err := mc.fetcher(context.Background())
	if err != nil {
		logger.Error("Failed to fetch mentors in background refresh", zap.Error(err))
		return err
	}

	// Update cache atomically
	mc.populateCache(mentors)

	mc.mu.Lock()
	mc.lastRefresh = time.Now()
	mc.mu.Unlock()

	duration := time.Since(startTime)
	logger.Info("Background refresh completed",
		zap.Int("count", len(mentors)),
		zap.Duration("duration", duration))

	return nil
}

// refreshWithRetry performs a refresh with exponential backoff retry logic
func (mc *MentorCache) refreshWithRetry() error {
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			//nolint:gosec // G115: attempt bounded by maxRetries (3), max shift is 2, no overflow possible
			waitTime := initialRetryWait * time.Duration(1<<uint(attempt-1)) // Exponential backoff
			logger.Info("Retrying cache refresh",
				zap.Int("attempt", attempt+1),
				zap.Int("max_attempts", maxRetries),
				zap.Duration("wait_time", waitTime))
			time.Sleep(waitTime)
		}

		// Fetch all mentors
		mentors, fetchErr := mc.fetcher(context.Background())
		if fetchErr != nil {
			err = fetchErr
			logger.Error("Cache refresh attempt failed",
				zap.Int("attempt", attempt+1),
				zap.Error(err))
			continue
		}

		// Populate cache
		mc.populateCache(mentors)

		return nil
	}

	return fmt.Errorf("failed to refresh cache after %d attempts: %w", maxRetries, err)
}

// populateCache stores all mentors in cache with individual keys
func (mc *MentorCache) populateCache(mentors []*models.Mentor) {
	slugs := make([]string, 0, len(mentors))

	for _, mentor := range mentors {
		key := mentorKeyPrefix + mentor.Slug

		// Store each mentor individually with NO expiration
		// Expiration is controlled at the "mentor:all" level
		mc.cache.Set(key, mentor, gocache.NoExpiration)

		slugs = append(slugs, mentor.Slug)
	}

	// Store slug list with TTL - this controls cache expiration
	mc.cache.Set(allMentorsKey, slugs, mc.ttl)

	// Store metadata
	mc.cache.Set(metadataKey, &CacheMetadata{
		LastRefreshTime: time.Now(),
		MentorCount:     len(mentors),
		Version:         time.Now().Unix(),
	}, gocache.NoExpiration)

	metrics.CacheSize.WithLabelValues("mentors").Set(float64(len(mentors)))

	logger.Info("Cache populated successfully", zap.Int("count", len(mentors)))
}

// ensureMentorInListLocked ensures slug is in all-mentors list
// MUST be called with mc.mu locked
func (mc *MentorCache) ensureMentorInListLocked(slug string) error {
	slugsData, found := mc.cache.Get(allMentorsKey)
	if !found {
		// List expired - will be recreated on next full refresh
		logger.Debug("All-mentors list not found, skipping update")
		return nil
	}

	slugs, ok := slugsData.([]string)
	if !ok {
		return fmt.Errorf("invalid all-mentors list type")
	}

	// Check if slug already exists
	for _, s := range slugs {
		if s == slug {
			return nil // Already in list
		}
	}

	// Add to list (preserve TTL)
	slugs = append(slugs, slug)
	mc.cache.Set(allMentorsKey, slugs, mc.ttl)

	return nil
}

// Clear clears the entire cache
func (mc *MentorCache) Clear() {
	mc.cache.Flush()
	logger.Info("Mentor cache cleared")
}

// GetMetadata returns cache metadata
func (mc *MentorCache) GetMetadata() (*CacheMetadata, error) {
	data, found := mc.cache.Get(metadataKey)
	if !found {
		return nil, fmt.Errorf("metadata not found")
	}

	metadata, ok := data.(*CacheMetadata)
	if !ok {
		return nil, fmt.Errorf("invalid metadata type")
	}

	return metadata, nil
}
