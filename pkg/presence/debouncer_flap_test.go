package presence

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestDebouncerRecordTransition_Flapping(t *testing.T) {
	mr := miniredis.RunT(t)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	svc := NewService(rdb)
	svc.SetUseHashTags(true)

	d := NewDebouncer(svc, nil, DebouncerConfig{
		FlappingWindow:    500 * time.Millisecond,
		FlappingThreshold: 3,
	})

	ctx := context.Background()
	scopeID := "scope1"
	userID := "user1"

	// First two transitions should not mark flapping.
	if flapping, err := d.recordTransition(ctx, scopeID, userID); err != nil || flapping {
		t.Fatalf("transition1 flapping=%v err=%v, want flapping=false err=nil", flapping, err)
	}
	if flapping, err := d.recordTransition(ctx, scopeID, userID); err != nil || flapping {
		t.Fatalf("transition2 flapping=%v err=%v, want flapping=false err=nil", flapping, err)
	}

	// Third transition within window triggers flapping (threshold=3).
	if flapping, err := d.recordTransition(ctx, scopeID, userID); err != nil || !flapping {
		t.Fatalf("transition3 flapping=%v err=%v, want flapping=true err=nil", flapping, err)
	}

	// After window passes, transitions should no longer be considered flapping.
	time.Sleep(600 * time.Millisecond)
	if flapping, err := d.recordTransition(ctx, scopeID, userID); err != nil || flapping {
		t.Fatalf("afterWindow flapping=%v err=%v, want flapping=false err=nil", flapping, err)
	}
}

