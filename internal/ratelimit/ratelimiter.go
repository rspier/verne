package ratelimit

import (
	"sync"
	"time"
)

// ipInfo stores the request count and the time of the first request from an IP.
type ipInfo struct {
	count     int
	firstSeen time.Time
}

// RateLimiter manages request counts for IPs and enforces rate limits.
type RateLimiter struct {
	ips       map[string]ipInfo
	mu        sync.Mutex
	threshold int
	period    time.Duration
	stopCh    chan struct{} // Channel to signal the cleanup goroutine to stop
}

// NewRateLimiter creates a new RateLimiter.
// It also starts a background goroutine to periodically clean up expired IP entries.
func NewRateLimiter(threshold int, period time.Duration) *RateLimiter {
	rl := &RateLimiter{
		ips:       make(map[string]ipInfo),
		threshold: threshold,
		period:    period,
		stopCh:    make(chan struct{}),
	}

	go rl.cleanupRoutine()
	return rl
}

// Allow checks if an IP is allowed to make a request.
// It increments the request count for the IP.
// Returns true if allowed, false if the IP has exceeded the threshold.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	info, exists := rl.ips[ip]

	// If IP is not tracked, or its period has expired, reset its tracking.
	if !exists || time.Since(info.firstSeen) > rl.period {
		rl.ips[ip] = ipInfo{
			count:     1,
			firstSeen: time.Now(),
		}
		return true // First request in a new period is always allowed (as long as threshold > 0)
	}

	// Increment count and check against threshold
	info.count++
	rl.ips[ip] = info // Update the map with the new count

	return info.count <= rl.threshold
}

// cleanupRoutine periodically removes expired IP entries from the map.
func (rl *RateLimiter) cleanupRoutine() {
	// Cleanup interval could be configurable or a fraction of the rate limit period
	// For simplicity, let's use the rate limit period itself as the cleanup interval.
	// A shorter interval (e.g., period / 2 or a fixed value like 1 minute) might be more responsive
	// in evicting old entries if memory is a major concern with many IPs.
	ticker := time.NewTicker(rl.period)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanupExpiredIPs()
		case <-rl.stopCh:
			return // Exit goroutine
		}
	}
}

// cleanupExpiredIPs iterates through the IP map and removes entries older than the configured period.
func (rl *RateLimiter) cleanupExpiredIPs() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, info := range rl.ips {
		if now.Sub(info.firstSeen) > rl.period {
			delete(rl.ips, ip)
		}
	}
}

// Stop gracefully shuts down the RateLimiter, stopping its cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}
