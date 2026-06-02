package eventbus_test

import (
	"server/peakgo/eventbus"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ─── Correctness ─────────────────────────────────────────────────────────────

func TestSubscribeAndPublish(t *testing.T) {
	bus := eventbus.New()
	received := make(chan eventbus.Event, 1)

	bus.Subscribe("test.topic", func(e eventbus.Event) {
		received <- e
	}, 0)

	bus.Publish("test.topic", "hello")

	select {
	case e := <-received:
		if e.(string) != "hello" {
			t.Fatalf("expected 'hello', got %v", e)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribersSameTopicAllReceive(t *testing.T) {
	bus := eventbus.New()
	const N = 3
	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		bus.Subscribe("broadcast.topic", func(e eventbus.Event) {
			count.Add(1)
			wg.Done()
		}, 0)
	}

	bus.Publish("broadcast.topic", struct{}{})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out — only %d/%d subscribers received event", count.Load(), N)
	}
	if count.Load() != N {
		t.Fatalf("expected %d deliveries, got %d", N, count.Load())
	}
}

func TestPublishToUnknownTopicIsNoOp(t *testing.T) {
	bus := eventbus.New()
	// Should not panic or block
	bus.Publish("nonexistent.topic", "event")
}

func TestPublishMultipleEventsOrdering(t *testing.T) {
	bus := eventbus.New()
	results := make([]int, 0, 5)
	var mu sync.Mutex
	done := make(chan struct{})
	expected := []int{1, 2, 3, 4, 5}

	bus.Subscribe("order.topic", func(e eventbus.Event) {
		mu.Lock()
		results = append(results, e.(int))
		if len(results) == 5 {
			close(done)
		}
		mu.Unlock()
	}, 16)

	for _, v := range expected {
		bus.Publish("order.topic", v)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for all events")
	}

	mu.Lock()
	defer mu.Unlock()
	for i, v := range expected {
		if results[i] != v {
			t.Fatalf("order mismatch at index %d: expected %d got %d", i, v, results[i])
		}
	}
}

func TestDropWhenChannelFull(t *testing.T) {
	bus := eventbus.New()
	// Subscribe with channel size 1 and a slow handler to force drops.
	var received atomic.Int32
	bus.Subscribe("slow.topic", func(e eventbus.Event) {
		time.Sleep(50 * time.Millisecond) // slow consumer
		received.Add(1)
	}, 1)

	// Publish 10 events rapidly — most should be dropped.
	for i := 0; i < 10; i++ {
		bus.Publish("slow.topic", i)
	}

	// Wait for the 1-2 that got through.
	time.Sleep(200 * time.Millisecond)
	got := received.Load()
	if got > 3 {
		t.Fatalf("expected at most 3 deliveries with slow consumer, got %d", got)
	}
}

func TestTopicAndSubscriberCount(t *testing.T) {
	bus := eventbus.New()
	if bus.TopicCount() != 0 {
		t.Fatal("expected 0 topics on new bus")
	}

	bus.Subscribe("a", func(e eventbus.Event) {}, 0)
	bus.Subscribe("a", func(e eventbus.Event) {}, 0)
	bus.Subscribe("b", func(e eventbus.Event) {}, 0)

	if bus.TopicCount() != 2 {
		t.Fatalf("expected 2 topics, got %d", bus.TopicCount())
	}
	if bus.SubscriberCount() != 3 {
		t.Fatalf("expected 3 subscribers, got %d", bus.SubscriberCount())
	}
}

func TestDrainStopsDelivery(t *testing.T) {
	bus := eventbus.New()
	var count atomic.Int32

	bus.Subscribe("drain.topic", func(e eventbus.Event) {
		count.Add(1)
	}, 4)

	bus.Publish("drain.topic", 1)
	time.Sleep(50 * time.Millisecond)
	bus.Drain()

	// Events published after Drain should be no-ops.
	bus.Publish("drain.topic", 2)
	time.Sleep(50 * time.Millisecond)

	if count.Load() > 1 {
		t.Fatalf("expected at most 1 delivery after drain, got %d", count.Load())
	}
}

func TestTypedEventRoundTrip(t *testing.T) {
	bus := eventbus.New()
	received := make(chan eventbus.MonsterDeathEvent, 1)

	bus.Subscribe(eventbus.TopicMonsterDeath, func(e eventbus.Event) {
		if ev, ok := e.(eventbus.MonsterDeathEvent); ok {
			received <- ev
		}
	}, 0)

	ev := eventbus.MonsterDeathEvent{
		MonsterID:   42,
		KillerID:    7,
		MonsterName: "Bandit",
		MapID:       1,
		XPReward:    50,
		TemplateID:  2,
	}
	bus.Publish(eventbus.TopicMonsterDeath, ev)

	select {
	case got := <-received:
		if got.MonsterID != 42 || got.KillerID != 7 || got.MonsterName != "Bandit" {
			t.Fatalf("event fields mismatch: %+v", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for MonsterDeathEvent")
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkPublishNoSubscribers(b *testing.B) {
	bus := eventbus.New()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish("empty.topic", i)
	}
}

func BenchmarkPublishOneSubscriber(b *testing.B) {
	bus := eventbus.New()
	bus.Subscribe("bench.topic", func(e eventbus.Event) {}, 1<<20) // huge buffer, never blocks
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish("bench.topic", i)
	}
}

func BenchmarkPublishThreeSubscribers(b *testing.B) {
	bus := eventbus.New()
	for i := 0; i < 3; i++ {
		bus.Subscribe("bench3.topic", func(e eventbus.Event) {}, 1<<20)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish("bench3.topic", i)
	}
}

func BenchmarkPublishConcurrent(b *testing.B) {
	bus := eventbus.New()
	bus.Subscribe("concurrent.topic", func(e eventbus.Event) {}, 1<<20)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bus.Publish("concurrent.topic", struct{}{})
		}
	})
}
