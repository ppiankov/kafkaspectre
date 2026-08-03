package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// WO-52: the concurrent offset-fetching path (WO-45) had zero test coverage.
// This test exercises fetchOffsetsForGroups with a fake fetcher and verifies
// the topic union, error handling, and concurrency under -race.
func TestFetchOffsetsForGroups(t *testing.T) {
	metadata := &ClusterMetadata{
		ConsumerGroups: map[string]*ConsumerGroupInfo{
			"cg-1": {GroupID: "cg-1", Topics: []string{"assigned-topic"}},
			"cg-2": {GroupID: "cg-2", Topics: []string{}},
			"cg-3": {GroupID: "cg-3", Topics: []string{"topic-a"}},
		},
	}

	groupIDs := []string{"cg-1", "cg-2", "cg-3"}

	// Fake fetcher: cg-1 has offsets for offset-topic, cg-2 fails, cg-3 has none.
	callCount := int32(0)
	fetcher := func(ctx context.Context, groupID string) (map[string]struct{}, error) {
		atomic.AddInt32(&callCount, 1)
		switch groupID {
		case "cg-1":
			return map[string]struct{}{"offset-topic": {}}, nil
		case "cg-2":
			return nil, errors.New("coordinator unavailable")
		case "cg-3":
			return map[string]struct{}{}, nil
		default:
			return nil, fmt.Errorf("unexpected group %q", groupID)
		}
	}

	fetchOffsetsForGroups(context.Background(), groupIDs, metadata, fetcher)

	if got := atomic.LoadInt32(&callCount); got != 3 {
		t.Fatalf("fetcher called %d times, want 3", got)
	}

	// cg-1: union of assigned + offset topics
	cg1 := metadata.ConsumerGroups["cg-1"]
	if len(cg1.Topics) != 2 {
		t.Fatalf("cg-1 topics = %v, want 2 (assigned + offset)", cg1.Topics)
	}
	want := map[string]bool{"assigned-topic": false, "offset-topic": false}
	for _, topic := range cg1.Topics {
		want[topic] = true
	}
	for topic, found := range want {
		if !found {
			t.Errorf("cg-1 missing topic %q", topic)
		}
	}

	// cg-2: failed fetch, error recorded, topics unchanged
	cg2 := metadata.ConsumerGroups["cg-2"]
	if len(cg2.Topics) != 0 {
		t.Fatalf("cg-2 topics = %v, want 0 (fetch failed)", cg2.Topics)
	}
	if len(metadata.ConsumerGroupReadErrors) != 1 {
		t.Fatalf("expected 1 read error, got %d", len(metadata.ConsumerGroupReadErrors))
	}

	// cg-3: no offset topics, keeps assignment only
	cg3 := metadata.ConsumerGroups["cg-3"]
	if len(cg3.Topics) != 1 || cg3.Topics[0] != "topic-a" {
		t.Fatalf("cg-3 topics = %v, want [topic-a]", cg3.Topics)
	}
}

// WO-52: verify the worker pool actually runs concurrently. If the fetcher is
// sequential, N fetches each taking 50ms would take N*50ms; with 16 workers,
// 50 fetches should complete in well under 50*50ms.
func TestFetchOffsetsForGroupsConcurrency(t *testing.T) {
	const numGroups = 50
	metadata := &ClusterMetadata{
		ConsumerGroups: make(map[string]*ConsumerGroupInfo, numGroups),
	}
	groupIDs := make([]string, numGroups)
	for i := 0; i < numGroups; i++ {
		id := fmt.Sprintf("cg-%d", i)
		metadata.ConsumerGroups[id] = &ConsumerGroupInfo{GroupID: id, Topics: []string{}}
		groupIDs[i] = id
	}

	fetcher := func(ctx context.Context, groupID string) (map[string]struct{}, error) {
		time.Sleep(50 * time.Millisecond)
		return map[string]struct{}{}, nil
	}

	start := time.Now()
	fetchOffsetsForGroups(context.Background(), groupIDs, metadata, fetcher)
	elapsed := time.Since(start)

	// Sequential would be 50 * 50ms = 2500ms. With 16 workers: ceil(50/16) * 50ms
	// = 4 * 50ms = 200ms. Allow generous headroom for CI.
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("fetchOffsetsForGroups took %v for %d groups; expected concurrent (< 1.5s)", elapsed, numGroups)
	}
}

// WO-52: context cancellation propagates to the fetcher.
func TestFetchOffsetsForGroupsContextCancellation(t *testing.T) {
	metadata := &ClusterMetadata{
		ConsumerGroups: map[string]*ConsumerGroupInfo{
			"cg-1": {GroupID: "cg-1", Topics: []string{}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	fetcher := func(ctx context.Context, groupID string) (map[string]struct{}, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return map[string]struct{}{}, nil
	}

	fetchOffsetsForGroups(ctx, []string{"cg-1"}, metadata, fetcher)

	if len(metadata.ConsumerGroupReadErrors) != 1 {
		t.Fatalf("expected 1 read error from cancelled context, got %d", len(metadata.ConsumerGroupReadErrors))
	}
}
