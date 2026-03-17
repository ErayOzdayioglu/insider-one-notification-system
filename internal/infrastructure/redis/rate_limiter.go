package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter controls the per-channel throughput of notification delivery.
type RateLimiter interface {
	// Allow checks whether one token is available for the given channel.
	// Returns true when the request is permitted, false when the caller
	// should back off. retryAfter indicates the minimum wait time before
	// a token becomes available (only meaningful when allowed is false).
	Allow(ctx context.Context, channel string) (allowed bool, retryAfter time.Duration, err error)

	// Wait blocks until a token is available for the given channel or the
	// context is cancelled.
	Wait(ctx context.Context, channel string) error
}

// rateLimitKeyPrefix is the Redis key prefix for per-channel rate limit
// buckets.
const rateLimitKeyPrefix = "ratelimit:"

// tokenBucketScript is a Lua script that atomically refills and consumes
// from a token bucket stored in a Redis hash. It uses two fields:
//
//   - tokens:      current number of tokens (float)
//   - last_refill: last refill timestamp in milliseconds (float)
//
// Arguments: KEYS[1] = bucket key, ARGV[1] = rate (tokens/sec),
// ARGV[2] = burst (max tokens), ARGV[3] = current time in ms.
//
// Returns: {allowed (0|1), wait_ms (float)} where wait_ms is the time in
// milliseconds until the next token becomes available.
var tokenBucketScript = redis.NewScript(`
local key       = KEYS[1]
local rate      = tonumber(ARGV[1])
local burst     = tonumber(ARGV[2])
local now_ms    = tonumber(ARGV[3])

local data = redis.call("HMGET", key, "tokens", "last_refill")
local tokens
local last_refill

if data[1] == false then
    -- First call: initialise the bucket full.
    tokens      = burst
    last_refill = now_ms
else
    tokens      = tonumber(data[1])
    last_refill = tonumber(data[2])
end

-- Refill based on elapsed time.
local elapsed_sec = (now_ms - last_refill) / 1000.0
local added       = elapsed_sec * rate
tokens            = math.min(tokens + added, burst)
last_refill       = now_ms

if tokens >= 1 then
    tokens = tokens - 1
    redis.call("HMSET", key, "tokens", tostring(tokens), "last_refill", tostring(last_refill))
    return {1, 0}
else
    -- Calculate how long until one token is available.
    local deficit    = 1 - tokens
    local wait_ms    = (deficit / rate) * 1000.0
    redis.call("HMSET", key, "tokens", tostring(tokens), "last_refill", tostring(last_refill))
    return {0, math.ceil(wait_ms)}
end
`)

// TokenBucketLimiter implements RateLimiter using a Redis-backed token
// bucket. Each channel has its own independent bucket.
type TokenBucketLimiter struct {
	client *redis.Client
	rate   int // tokens per second
	burst  int // maximum tokens in the bucket
}

// NewTokenBucketLimiter creates a rate limiter with the given per-second
// rate and burst size. Both values must be > 0.
func NewTokenBucketLimiter(client *redis.Client, rate, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		client: client,
		rate:   rate,
		burst:  burst,
	}
}

// Allow atomically checks and consumes a token for the given channel.
func (l *TokenBucketLimiter) Allow(ctx context.Context, channel string) (bool, time.Duration, error) {
	key := rateLimitKeyPrefix + channel
	nowMS := time.Now().UnixMilli()

	result, err := tokenBucketScript.Run(ctx, l.client, []string{key},
		l.rate, l.burst, nowMS,
	).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("rate limiter lua script for channel %s: %w", channel, err)
	}

	allowed := result[0] == 1
	waitMS := result[1]

	return allowed, time.Duration(waitMS) * time.Millisecond, nil
}

// Wait blocks until a token is available for the given channel. If the
// context is cancelled while waiting, the context error is returned.
func (l *TokenBucketLimiter) Wait(ctx context.Context, channel string) error {
	for {
		allowed, retryAfter, err := l.Allow(ctx, channel)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}

		// Ensure we always wait at least a small amount to avoid hot loops
		// if the script returns 0 ms.
		if retryAfter < time.Millisecond {
			retryAfter = time.Millisecond
		}

		timer := time.NewTimer(retryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Retry.
		}
	}
}
