package infracache

import (
	applicationinteraction "GCFeed/internal/application/interaction"
	domaininteraction "GCFeed/internal/domain/interaction"
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newLuaFeedCache(t *testing.T) *FeedCache {
	t.Helper()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return NewFeedCache(client)
}

func TestSetActionStateLuaIsIdempotent(t *testing.T) {
	ctx := context.Background()
	cache := newLuaFeedCache(t)
	initial := &domaininteraction.VideoStat{
		VideoID:       1001,
		LikeCount:     10,
		CommentCount:  2,
		FavoriteCount: 3,
	}

	liked, err := cache.SetActionState(ctx, 42, 1001, domaininteraction.ActionTypeLike, true, "like-1", initial)
	if err != nil {
		t.Fatalf("like: %v", err)
	}
	if !liked.Active || liked.LikeCount != 11 || liked.Delta != 1 {
		t.Fatalf("unexpected first like result: %+v", liked)
	}

	replayed, err := cache.SetActionState(ctx, 42, 1001, domaininteraction.ActionTypeLike, true, "like-1", initial)
	if err != nil {
		t.Fatalf("replay like: %v", err)
	}
	if !replayed.Active || replayed.LikeCount != 11 || replayed.Delta != 0 {
		t.Fatalf("unexpected replayed like result: %+v", replayed)
	}

	unliked, err := cache.SetActionState(ctx, 42, 1001, domaininteraction.ActionTypeLike, false, "like-2", initial)
	if err != nil {
		t.Fatalf("unlike: %v", err)
	}
	if unliked.Active || unliked.LikeCount != 10 || unliked.Delta != -1 {
		t.Fatalf("unexpected unlike result: %+v", unliked)
	}
}

func TestSetActionStateLuaPreventsConcurrentDuplicateCount(t *testing.T) {
	ctx := context.Background()
	cache := newLuaFeedCache(t)
	initial := &domaininteraction.VideoStat{VideoID: 1002, LikeCount: 10}

	const goroutines = 32
	results := make(chan *applicationinteraction.ActionStateResult, goroutines)
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for index := 0; index < goroutines; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := cache.SetActionState(ctx, 77, 1002, domaininteraction.ActionTypeLike, true, "concurrent-like-1", initial)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("concurrent like: %v", err)
	}

	deltaTotal := 0
	for result := range results {
		deltaTotal += result.Delta
		if !result.Active || result.LikeCount != 11 {
			t.Fatalf("unexpected concurrent result: %+v", result)
		}
	}
	if deltaTotal != 1 {
		t.Fatalf("expected one effective like, got delta total %d", deltaTotal)
	}
}
