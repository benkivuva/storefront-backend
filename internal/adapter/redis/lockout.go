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

// BackoffStep is one entry in the escalating-backoff schedule.
type BackoffStep struct {
	MinFailures int
	Wait        time.Duration
}

// LockoutStore implements domain.LockoutStore. Failures are keyed on
// (userID, ipPrefix) so one network cannot lock a user out from a different
// network. Waits escalate with the failure count; the record expires after
// recordTTL so a user who walks away gets a fresh start.
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

func (l *LockoutStore) Backoff(ctx context.Context, userID domain.ID, ipPrefix string) (time.Duration, error) {
	untilMs, err := l.client.HGet(ctx, lockoutKey(userID, ipPrefix), "until").Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("lockout backoff: %w", err)
	}
	wait := untilMs - time.Now().UnixMilli()
	if wait <= 0 {
		return 0, nil
	}
	return time.Duration(wait) * time.Millisecond, nil
}

func (l *LockoutStore) RecordFailedAttempt(ctx context.Context, userID domain.ID, ipPrefix string) (time.Duration, error) {
	args := make([]any, 0, 2+len(l.schedule)*2)
	args = append(args, time.Now().UnixMilli(), int64(l.recordTTL.Seconds()))
	for _, s := range l.schedule {
		args = append(args, int64(s.MinFailures), s.Wait.Milliseconds())
	}
	waitMs, err := l.script.Run(ctx, l.client, []string{lockoutKey(userID, ipPrefix)}, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("lockout record: %w", err)
	}
	return time.Duration(waitMs) * time.Millisecond, nil
}

func (l *LockoutStore) Reset(ctx context.Context, userID domain.ID, ipPrefix string) error {
	if err := l.client.Del(ctx, lockoutKey(userID, ipPrefix)).Err(); err != nil {
		return fmt.Errorf("lockout reset: %w", err)
	}
	return nil
}

func lockoutKey(userID domain.ID, ipPrefix string) string {
	return "lockout:" + userID.String() + ":" + ipPrefix
}

const recordFailedLua = `
local key        = KEYS[1]
local now_ms     = tonumber(ARGV[1])
local record_ttl = tonumber(ARGV[2])

local thresholds = {}
for i = 3, #ARGV, 2 do
    thresholds[#thresholds+1] = {tonumber(ARGV[i]), tonumber(ARGV[i+1])}
end

local count = tonumber(redis.call('HGET', key, 'count')) or 0
count = count + 1

local new_wait = 0
for _, pair in ipairs(thresholds) do
    if count >= pair[1] then
        new_wait = pair[2]
    end
end

redis.call('HSET', key, 'count', count, 'until', now_ms + new_wait)
redis.call('EXPIRE', key, record_ttl)

return new_wait
`
