package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

var (
	ErrEmptySchedule    = errors.New("lockout: schedule must not be empty")
	ErrInvalidThreshold = errors.New("lockout: thresholds must be positive and strictly increasing")
	ErrInvalidWait      = errors.New("lockout: waits must be positive")
	ErrInvalidRecordTTL = errors.New("lockout: record TTL must be positive and >= max wait")
)

type BackoffStep struct {
	MinFailures int
	Wait        time.Duration
}

type LockoutStore struct {
	client    goredis.UniversalClient
	schedule  []BackoffStep
	recordTTL time.Duration
	script    *goredis.Script
}

func NewLockoutStore(client goredis.UniversalClient, schedule []BackoffStep, recordTTL time.Duration) (*LockoutStore, error) {
	if len(schedule) == 0 {
		return nil, ErrEmptySchedule
	}
	prev := 0
	for _, s := range schedule {
		if s.MinFailures <= prev {
			return nil, fmt.Errorf("%w: MinFailures=%d", ErrInvalidThreshold, s.MinFailures)
		}
		if s.Wait <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidWait, s.Wait)
		}
		prev = s.MinFailures
	}
	if recordTTL <= 0 {
		return nil, ErrInvalidRecordTTL
	}
	if maxWait := schedule[len(schedule)-1].Wait; recordTTL < maxWait {
		return nil, fmt.Errorf("%w: %s < max wait %s", ErrInvalidRecordTTL, recordTTL, maxWait)
	}
	return &LockoutStore{
		client:    client,
		schedule:  schedule,
		recordTTL: recordTTL,
		script:    goredis.NewScript(recordFailedLua),
	}, nil
}

// Backoff checks both the IP-specific bucket and the global account bucket.
// It returns the longer of the two waits.
func (l *LockoutStore) Backoff(ctx context.Context, userID domain.ID, ipPrefix string) (time.Duration, error) {
	ipKey := ipLockoutKey(userID, ipPrefix)
	acctKey := accountLockoutKey(userID)
	now := time.Now().UnixMilli()

	ipWait, err := l.getRemaining(ctx, ipKey, now)
	if err != nil {
		return 0, err
	}
	acctWait, err := l.getRemaining(ctx, acctKey, now)
	if err != nil {
		return 0, err
	}

	if acctWait > ipWait {
		return acctWait, nil
	}
	return ipWait, nil
}

func (l *LockoutStore) getRemaining(ctx context.Context, key string, nowMs int64) (time.Duration, error) {
	untilMs, err := l.client.HGet(ctx, key, "until").Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("lockout backoff: %w", err)
	}
	wait := untilMs - nowMs
	if wait <= 0 {
		return 0, nil
	}
	return time.Duration(wait) * time.Millisecond, nil
}

// RecordFailedAttempt increments both the IP and account counters atomically
// and returns the maximum applicable wait.
func (l *LockoutStore) RecordFailedAttempt(ctx context.Context, userID domain.ID, ipPrefix string) (time.Duration, error) {
	args := make([]any, 0, 2+len(l.schedule)*2)
	args = append(args, time.Now().UnixMilli(), int64(l.recordTTL.Seconds()))
	for _, s := range l.schedule {
		args = append(args, int64(s.MinFailures), s.Wait.Milliseconds())
	}

	keys := []string{ipLockoutKey(userID, ipPrefix), accountLockoutKey(userID)}
	waitMs, err := l.script.Run(ctx, l.client, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("lockout record: %w", err)
	}
	return time.Duration(waitMs) * time.Millisecond, nil
}

func (l *LockoutStore) Reset(ctx context.Context, userID domain.ID, ipPrefix string) error {
	keys := []string{ipLockoutKey(userID, ipPrefix), accountLockoutKey(userID)}
	if err := l.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("lockout reset: %w", err)
	}
	return nil
}

func ipLockoutKey(userID domain.ID, ipPrefix string) string {
	return "lockout:ip:" + userID.String() + ":" + ipPrefix
}

func accountLockoutKey(userID domain.ID) string {
	return "lockout:acct:" + userID.String()
}

const recordFailedLua = `
local key_ip     = KEYS[1]
local key_acct   = KEYS[2]
local now_ms     = tonumber(ARGV[1])
local record_ttl = tonumber(ARGV[2])

local thresholds = {}
for i = 3, #ARGV, 2 do
    thresholds[#thresholds+1] = {tonumber(ARGV[i]), tonumber(ARGV[i+1])}
end

-- Increment IP-specific
local count_ip = tonumber(redis.call('HGET', key_ip, 'count')) or 0
count_ip = count_ip + 1
local wait_ip = 0
for _, pair in ipairs(thresholds) do
    if count_ip >= pair[1] then wait_ip = pair[2] end
end
redis.call('HSET', key_ip, 'count', count_ip, 'until', now_ms + wait_ip)
redis.call('EXPIRE', key_ip, record_ttl)

-- Increment Account-wide
local count_acct = tonumber(redis.call('HGET', key_acct, 'count')) or 0
count_acct = count_acct + 1
local wait_acct = 0
for _, pair in ipairs(thresholds) do
    if count_acct >= pair[1] then wait_acct = pair[2] end
end
redis.call('HSET', key_acct, 'count', count_acct, 'until', now_ms + wait_acct)
redis.call('EXPIRE', key_acct, record_ttl)

if wait_acct > wait_ip then
    return wait_acct
end
return wait_ip
`
