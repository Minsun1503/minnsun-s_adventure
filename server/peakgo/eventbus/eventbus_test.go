package eventbus_test

import (
	"server/peakgo/eventbus"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSubscribeAndPublish verifies the most basic pub/sub loop:
// Registering a subscriber, publishing a single message, and asserting content.
func TestSubscribeAndPublish(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain() // Ensures background workers are terminated cleanly after test

	received := make(chan string, 1)

	bus.Subscribe("test", func(e eventbus.Event) {
		received <- e.(string)
	}, 0)

	bus.Publish("test", "hello")

	select {
	case got := <-received:
		if got != "hello" {
			t.Fatalf("expected hello, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestMultipleSubscribersReceiveEvent ensures that when an event is published,
// ALL subscribers listening to that specific topic receive exactly one copy (Fan-out pattern).
func TestMultipleSubscribersReceiveEvent(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain()

	const subscribers = 5

	var wg sync.WaitGroup
	wg.Add(subscribers)

	for i := 0; i < subscribers; i++ {
		bus.Subscribe("broadcast", func(e eventbus.Event) {
			wg.Done()
		}, 0)
	}

	bus.Publish("broadcast", struct{}{})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not all subscribers received event")
	}
}

// TestPublishUnknownTopic verifies that publishing an event to a topic
// with zero registered subscribers is a safe, non-blocking no-op.
func TestPublishUnknownTopic(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain()

	bus.Publish("unknown", struct{}{})
}

// TestOrderingPerSubscriber guarantees the strict FIFO delivery rule.
// Events dispatched sequentially must land in the exact same sequence at the subscriber.
func TestOrderingPerSubscriber(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain()

	expected := []int{1, 2, 3, 4, 5}

	var (
		mu      sync.Mutex
		results []int
		done    = make(chan struct{})
	)

	bus.Subscribe("order", func(e eventbus.Event) {
		mu.Lock()
		defer mu.Unlock()

		results = append(results, e.(int))

		if len(results) == len(expected) {
			close(done)
		}
	}, 16)

	for _, v := range expected {
		bus.Publish("order", v)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ordered events")
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range expected {
		if results[i] != expected[i] {
			t.Fatalf(
				"index=%d expected=%d got=%d",
				i,
				expected[i],
				results[i],
			)
		}
	}
}

// TestDropWhenChannelFull validates the critical "At-Most-Once" delivery rule.
// It explicitly blocks the consumer worker, fills up the channel buffer (capacity 1),
// floods the bus, and verifies that overflowing events are safely dropped without stalling the engine.
func TestDropWhenChannelFull(t *testing.T) {
	bus := eventbus.New()

	var delivered atomic.Int32

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})

	// Subscribe with a tiny buffer size of 1
	bus.Subscribe("slow", func(e eventbus.Event) {
		if e == "blocker" {
			close(handlerStarted)
			<-releaseHandler // Freezes the worker completely
			return
		}

		delivered.Add(1)
	}, 1)

	// 1. Send the blocker packet to freeze the consumer
	bus.Publish("slow", "blocker")
	<-handlerStarted // Wait until the worker is locked

	// 2. This event occupies the 1 available slot in the buffer channel
	bus.Publish("slow", "buffered")

	// 3. Flood the bus. Since the worker is frozen and the buffer is full,
	// all 100 packets must be thrown away instantly (non-blocking drop)
	for i := 0; i < 100; i++ {
		bus.Publish("slow", i)
	}

	// 4. Release the consumer freeze and let the bus flush
	close(releaseHandler)
	bus.Drain() // Wait for the remaining "buffered" event to process

	// Only "buffered" should make it through. All 100 loops must be dropped.
	if delivered.Load() != 1 {
		t.Fatalf(
			"expected exactly one buffered event, got %d",
			delivered.Load(),
		)
	}
}

// TestDrainStopsFurtherPublish evaluates the lifecycle isolation.
// Once Drain() triggers, the system turns away new incoming packages instantly.
func TestDrainStopsFurtherPublish(t *testing.T) {
	bus := eventbus.New()

	var count atomic.Int32

	bus.Subscribe("drain", func(e eventbus.Event) {
		count.Add(1)
	}, 8)

	bus.Publish("drain", 1) // Safe delivery

	bus.Drain() // Closes the bus completely

	bus.Publish("drain", 2) // Must be blocked and ignored safely

	if count.Load() != 1 {
		t.Fatalf(
			"expected 1 delivery, got %d",
			count.Load(),
		)
	}
}

// TestTopicAndSubscriberCount verifies internal reporting counters are reporting accurate data maps.
func TestTopicAndSubscriberCount(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain()

	bus.Subscribe("a", func(e eventbus.Event) {}, 0)
	bus.Subscribe("a", func(e eventbus.Event) {}, 0)
	bus.Subscribe("b", func(e eventbus.Event) {}, 0)

	if bus.TopicCount() != 2 {
		t.Fatalf(
			"expected 2 topics, got %d",
			bus.TopicCount(),
		)
	}

	if bus.SubscriberCount() != 3 {
		t.Fatalf(
			"expected 3 subscribers, got %d",
			bus.SubscriberCount(),
		)
	}
}

// TestTypedEventRoundTrip verifies the real-world payload integrity.
// Asserts that specific struct fields pass correctly without truncation or corruption.
func TestTypedEventRoundTrip(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain()

	received := make(chan eventbus.MonsterDeathEvent, 1)

	bus.Subscribe(
		eventbus.TopicMonsterDeath,
		func(e eventbus.Event) {
			received <- e.(eventbus.MonsterDeathEvent)
		},
		0,
	)

	expected := eventbus.MonsterDeathEvent{
		MonsterID:   42,
		KillerID:    7,
		MonsterName: "Bandit",
		MapID:       1,
		XPReward:    50,
		TemplateID:  2,
	}

	bus.Publish(
		eventbus.TopicMonsterDeath,
		expected,
	)

	select {
	case got := <-received:
		if got != expected {
			t.Fatalf(
				"expected %+v got %+v",
				expected,
				got,
			)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// TestConcurrentPublish executes intensive multi-threaded publication workloads.
// Designed to be triggered with the '-race' detector flag to catch data racing hazards.
func TestConcurrentPublish(t *testing.T) {
	bus := eventbus.New()
	defer bus.Drain()

	var count atomic.Int64

	bus.Subscribe("concurrent", func(e eventbus.Event) {
		count.Add(1)
	}, 100000)

	const goroutines = 16
	const publishes = 1000

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < publishes; i++ {
				bus.Publish("concurrent", i)
			}
		}()
	}

	wg.Wait()
	bus.Drain()

	if count.Load() == 0 {
		t.Fatal("expected events to be delivered")
	}
}

// TestPublishWhileDrain tests an extreme edge case scenario:
// Massive publication pipelines executing precisely while the system calls Drain().
// Ensures no race-panics occur under shutdown contention.
func TestPublishWhileDrain(t *testing.T) {
	bus := eventbus.New()

	bus.Subscribe("race", func(e eventbus.Event) {}, 1024)

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 10000; j++ {
				bus.Publish("race", j)
			}
		}()
	}

	go bus.Drain()

	wg.Wait()
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

// BenchmarkPublishNoSubscribers benchmarks the pure overhead of looking up a topic
// inside the internal map when no subscribers exist.
func BenchmarkPublishNoSubscribers(b *testing.B) {
	bus := eventbus.New()
	defer bus.Drain()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bus.Publish("empty", i)
	}
}

// BenchmarkPublishOneSubscriber records processing throughput and allocation spikes
// with exactly 1 fast listener attached to a massive unblocking buffer.
func BenchmarkPublishOneSubscriber(b *testing.B) {
	bus := eventbus.New()
	defer bus.Drain()

	bus.Subscribe("bench", func(e eventbus.Event) {}, 1<<20)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bus.Publish("bench", i)
	}
}

// BenchmarkPublishThreeSubscribers benchmarks scaling behaviors under typical fan-out operations (3 subscribers).
func BenchmarkPublishThreeSubscribers(b *testing.B) {
	bus := eventbus.New()
	defer bus.Drain()

	for i := 0; i < 3; i++ {
		bus.Subscribe("bench3", func(e eventbus.Event) {}, 1<<20)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bus.Publish("bench3", i)
	}
}

// BenchmarkPublishHundredSubscribers measures routing stress limits and latency degradation
// when broadcasting a single event to a massive listener cluster (100 subscribers).
func BenchmarkPublishHundredSubscribers(b *testing.B) {
	bus := eventbus.New()
	defer bus.Drain()

	for i := 0; i < 100; i++ {
		bus.Subscribe("bench100", func(e eventbus.Event) {}, 1<<20)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bus.Publish("bench100", i)
	}
}

// BenchmarkPublishConcurrent executes intense parallel thread loops to evaluate
// synchronization lock contention on the bus's internal RWMutex under max loads.
func BenchmarkPublishConcurrent(b *testing.B) {
	bus := eventbus.New()
	defer bus.Drain()

	bus.Subscribe(
		"parallel",
		func(e eventbus.Event) {},
		1<<20,
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bus.Publish("parallel", struct{}{})
		}
	})
}
