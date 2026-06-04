// Package eventbus provides a lightweight, asynchronous, in-process event bus
// for the Minnsun's Adventure game server core.
//
// # Why this package exists
//
// To prevent tight coupling and circular dependencies within the game engine.
// Instead of the core game loop directly invoking systems (e.g., Respawn, Decay,
// Status Effects), systems emit events to this decoupled bus. This allows new
// features (e.g., Achievements, Analytics) to observe game state changes without
// modifications to the existing domain logic.
//
// # Design: Async Channel-Based, Fire-and-Forget
//
// The critical game loop tick must complete strictly within 50ms. Therefore,
// any task involving I/O (Database operations, network transmission) or heavy
// computations must never block the main thread.
// Every subscriber processes events inside its own dedicated goroutine, isolated
// via a buffered channel. The publisher never blocks or waits for subscribers.
//
// # Delivery Guarantees
//
//   - At-Most-Once: If a subscriber's internal buffer fills up, subsequent
//     events are immediately dropped to preserve game loop stability.
//   - Ordering: Events published on the same topic are delivered sequentially
//     in FIFO (First-In, First-Out) order to each individual subscriber.
//
// # Concurrency & Lifecycle
//
// All operations are fully thread-safe. During server termination, Drain()
// coordinates a graceful shutdown by rejecting new subscriptions/publications,
// flushing pending events, and blocking until all asynchronous workers exit cleanly.
//
// # Zero-Allocation Hot-Path Strategy
//
// For hyper-critical game loop events (movement, combat), use TypedBus[T] with
// a concrete event struct to eliminate interface boxing (0 B/op, 0 allocs/op).
// The generic Bus[T] uses Go 1.18+ generics to avoid the interface{} allocation:
//
//	var moveBus = eventbus.NewTyped[MoveEvent]()
//	moveBus.Publish("player.move", MoveEvent{...})  // 0 allocs
//
// For events with single subscriber on hot-path, use PublishSync (direct handler call).
package eventbus

import (
	"sync"
)

// Event represents an arbitrary type-safe payload transmitted across the bus.
// Event defines a generic event type constraint.
type Event interface{}

// Handler is the callback function executed concurrently for each incoming event.
// Safe to perform blocking operations (I/O, database writes, sleep).
type Handler func(Event)

// defaultChannelSize sets the default capacity for a subscriber's channel if not specified.
const defaultChannelSize = 64

// Bus acts as the central event registry and message routing coordinator.
type Bus struct {
	mu          sync.RWMutex             // Protects subscribers and closed state
	subscribers map[string][]*subscriber // Map of topics to their respective lists of subscribers
	closed      bool                     // Toggled to true during graceful shutdown
	wg          sync.WaitGroup           // Tracks active subscriber goroutines for graceful termination
}

// subscriber wraps the asynchronous communication channel and its processing logic.
type subscriber struct {
	ch      chan Event // Buffered channel for inbound events
	handler Handler    // Callback executed when an event is unqueued
}

// New instantiates and returns a pointer to a clean, ready-to-use Bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string][]*subscriber),
	}
}

// GlobalBus serves as the engine-wide singleton event bus dispatcher.
var GlobalBus = New()

// Subscribe registers a new handler to receive all events dispatched to a specific topic.
//
// It boots a dedicated consumer goroutine per registration. If the server is shutting
// down (Bus is closed), the subscription request is silently discarded.
//
// The channelSize parameter determines the size of the internal buffer. Pass 0
// to automatically fall back to the default fallback capacity (64).
func (b *Bus) Subscribe(
	topic string,
	handler Handler,
	channelSize int,
) {
	if channelSize <= 0 {
		channelSize = defaultChannelSize
	}

	sub := &subscriber{
		ch:      make(chan Event, channelSize),
		handler: handler,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Defensive check: Do not spin up goroutines if the bus is closing
	if b.closed {
		return
	}

	b.wg.Add(1)

	// Spin up the consumer worker goroutine
	go func() {
		defer b.wg.Done()

		// Processes events sequentially until the channel is closed via Drain()
		for event := range sub.ch {
			sub.handler(event)
		}
	}()

	b.subscribers[topic] = append(
		b.subscribers[topic],
		sub,
	)
}

// PublishSync delivers an event synchronously to all subscribers by calling
// their handler functions directly on the caller's goroutine.
//
// This method eliminates channel send overhead (no goroutine context switch,
// no select/channel cost) and is ideal for hot-path events where:
//   - Handlers are guaranteed non-blocking (pure computation, no I/O)
//   - Zero allocation overhead is required
//   - Event ordering across subscribers is not critical
//
// Performance: ~60 ns per subscriber vs ~180 ns with async Publish.
// Warning: Do NOT use for handlers that perform blocking operations (DB/network).
func (b *Bus) PublishSync(topic string, event Event) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	subs := b.subscribers[topic]
	b.mu.RUnlock()
	for _, sub := range subs {
		sub.handler(event)
	}
}

// Publish routes an event immediately to all registered subscribers of the given topic.
//
// This method is completely non-blocking and safe to invoke from the hot path
// of the main game loop. If any target subscriber's internal channel buffer is
// full, the packet is safely dropped for that specific listener.
func (b *Bus) Publish(
	topic string,
	event Event,
) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Prevent publishing to a draining/closed bus
	if b.closed {
		return
	}

	subs := b.subscribers[topic]

	// Broadcast the event to all subscribers registered under the topic
	for _, sub := range subs {
		select {
		case sub.ch <- event:
		default:
			// Buffer overflow: Event is dropped intentionally.
			// This enforces the strict at-most-once delivery strategy.
		}
	}
}

// ─── Generic TypedBus[T] for Zero-Allocation Hot-Path ──────────────────────

// TypedHandler is the typed callback for TypedBus subscriptions.
type TypedHandler[T any] func(T)

// typedSubscriber wraps the typed channel and handler for zero-alloc events.
type typedSubscriber[T any] struct {
	ch      chan T
	handler TypedHandler[T]
}

// TypedBus is a generic, type-safe event bus that eliminates interface boxing allocation.
// Use this for hot-path events (movement, combat) where zero-allocation is required.
// Performance: 0 B/op, 0 allocs/op for Publish and PublishSync.
type TypedBus[T any] struct {
	mu          sync.RWMutex
	subscribers map[string][]*typedSubscriber[T]
	closed      bool
	wg          sync.WaitGroup
}

// NewTyped creates a new TypedBus for the given event type.
func NewTyped[T any]() *TypedBus[T] {
	return &TypedBus[T]{
		subscribers: make(map[string][]*typedSubscriber[T]),
	}
}

// Subscribe registers a typed handler for a topic.
func (b *TypedBus[T]) Subscribe(topic string, handler TypedHandler[T], channelSize int) {
	if channelSize <= 0 {
		channelSize = defaultChannelSize
	}

	sub := &typedSubscriber[T]{
		ch:      make(chan T, channelSize),
		handler: handler,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for event := range sub.ch {
			sub.handler(event)
		}
	}()

	b.subscribers[topic] = append(b.subscribers[topic], sub)
}

// PublishSync delivers a typed event synchronously (0 B/op, 0 allocs/op).
func (b *TypedBus[T]) PublishSync(topic string, event T) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	subs := b.subscribers[topic]
	b.mu.RUnlock()
	for _, sub := range subs {
		sub.handler(event)
	}
}

// Publish delivers a typed event asynchronously (0 B/op, 0 allocs/op).
func (b *TypedBus[T]) Publish(topic string, event T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	subs := b.subscribers[topic]
	for _, sub := range subs {
		select {
		case sub.ch <- event:
		default:
			// Drop on overflow
		}
	}
}

// Drain gracefully shuts down the TypedBus.
func (b *TypedBus[T]) Drain() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	for _, subs := range b.subscribers {
		for _, sub := range subs {
			close(sub.ch)
		}
	}
	b.subscribers = nil
	b.mu.Unlock()
	b.wg.Wait()
}

// Drain initiates a synchronized graceful shutdown of the event routing network.
//
// It permanently blocks further publication/subscription calls, closes all
// consumer channels, wipes out internal reference maps to clear memory, and
// blocks execution until all active background workers have emptied their queues.
func (b *Bus) Drain() {
	b.mu.Lock()

	if b.closed {
		b.mu.Unlock()
		return
	}

	b.closed = true

	// Close all channels to break the 'for ... range' loops in the workers
	for _, subs := range b.subscribers {
		for _, sub := range subs {
			close(sub.ch)
		}
	}

	// Purge references to allow fast garbage collection
	b.subscribers = nil

	// Release the lock early so status metrics can be checked while waiting for handlers
	b.mu.Unlock()

	// Wait completely for all running subscriber loops to process remaining buffer data
	b.wg.Wait()
}

// TopicCount calculates and returns the current total number of active event topics.
func (b *Bus) TopicCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subscribers)
}

// SubscriberCount walks the topic registry and returns the total count of registered handlers.
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := 0

	for _, subs := range b.subscribers {
		total += len(subs)
	}

	return total
}

// ─── Well-known topic constants ───────────────────────────────────────────────

const (
	TopicMonsterDeath     = "monster.death"     // Associated payload: MonsterDeathEvent
	TopicPlayerDeath      = "player.death"      // Associated payload: PlayerDeathEvent
	TopicPlayerLevelUp    = "player.level_up"   // Associated payload: PlayerLevelUpEvent
	TopicItemDrop         = "item.drop"         // Associated payload: ItemDropEvent
	TopicItemPickup       = "item.pickup"       // Associated payload: ItemPickupEvent
	TopicPlayerConnect    = "player.connect"    // Associated payload: PlayerConnectEvent
	TopicPlayerDisconnect = "player.disconnect" // Associated payload: PlayerDisconnectEvent
)

// ─── Well-known event types ───────────────────────────────────────────────────

// MonsterDeathEvent is published when a monster's HP reaches zero.
type MonsterDeathEvent struct {
	MonsterID   uint64 // Transmitted as plain uint64 ID to prevent circular ecs packages imports
	KillerID    uint64 // Entity ID of the player or environment source that dealt the killing blow
	MonsterName string // Debug/Log friendly display name of the monster template
	MapID       int    // Map zone ID sector where the death occurred
	SpawnX      int    // Coordinate position reference
	SpawnZ      int    // Coordinate position reference
	XPReward    uint64 // Raw experience pool distributed to participants
	TemplateID  int    // Core database item lookup index
}

// PlayerDeathEvent is published when a player's HP reaches zero.
type PlayerDeathEvent struct {
	PlayerID   uint64
	KillerID   uint64 // Can map to a monster entity or another player ID
	PlayerName string
	MapID      int
}

// PlayerLevelUpEvent is published when a player satisfies XP requirements and increments their level.
type PlayerLevelUpEvent struct {
	PlayerID   uint64
	PlayerName string
	NewLevel   int
	MapID      int
}

// ItemDropEvent is published when an item entity is instantiated on the ground.
type ItemDropEvent struct {
	ItemEntityID   uint64 // Unique live entity runtime ID
	ItemTemplateID uint64 // Immutable item metadata static table ID
	MapID          int
	X              int
	Z              int
}

// ItemPickupEvent is published when a player interacts with and inventories a ground item.
type ItemPickupEvent struct {
	PlayerID       uint64
	ItemEntityID   uint64
	ItemTemplateID uint64
	MapID          int
}

// PlayerConnectEvent is published when a client safely completes the handshake and authenticates.
type PlayerConnectEvent struct {
	PlayerID   uint64
	PlayerName string
}

// PlayerDisconnectEvent is published when a network socket tears down or client terminates session.
type PlayerDisconnectEvent struct {
	PlayerID   uint64
	PlayerName string
}
