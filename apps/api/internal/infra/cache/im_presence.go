package infracache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type IMPresenceCache struct {
	client redis.Cmdable
}

func NewIMPresenceCache(client redis.Cmdable) *IMPresenceCache {
	return &IMPresenceCache{client: client}
}

func (c *IMPresenceCache) SetOnline(ctx context.Context, userID int64, ttl time.Duration) error {
	if c == nil || c.client == nil || userID <= 0 || ttl <= 0 {
		return nil
	}
	return c.client.Set(ctx, imPresenceKey(userID), "1", ttl).Err()
}

func (c *IMPresenceCache) SetOffline(ctx context.Context, userID int64) error {
	if c == nil || c.client == nil || userID <= 0 {
		return nil
	}
	return c.client.Del(ctx, imPresenceKey(userID)).Err()
}

func (c *IMPresenceCache) IsOnline(ctx context.Context, userID int64) (bool, error) {
	if c == nil || c.client == nil || userID <= 0 {
		return false, nil
	}
	count, err := c.client.Exists(ctx, imPresenceKey(userID)).Result()
	return count > 0, err
}

func imPresenceKey(userID int64) string {
	return fmt.Sprintf("im:presence:v1:%d", userID)
}
