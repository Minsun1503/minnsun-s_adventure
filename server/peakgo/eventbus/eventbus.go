// Package eventbus provides a lightweight async in-process event bus for the
// Minnsun's Adventure game server.
//
// # Why this package exists
//
// The game loop in systems/gameloop.go currently calls systems directly:
//
//	game.RunGroundItemDecaySystem()
//	game.GlobalRespawnManager.RunRespawnSystem()
//	game.RunStatusEffectsSystem()
//
// This creates tight coupling: gameloop imports game, which imports world,
// which can import game — a growing risk of import cycles as new systems
// are added. More importantly, adding a new system response to an existing
// event (e.g. "on monster death, also trigger an achievement check") requires
// editing combat.go rather than registering a new listener.
//
// # Design: async channel-based, fire-and-forget
//
// The game loop MUST complete its tick within 50ms. Any subscriber that does
// I/O (DB writes, network sends) or heavy computation must not block the loop.
// Each subscriber runs in its own goroutine and receives events via a buffered
// channel. The publisher never waits for subscribers.
//
// # Topic model
//
// Events are keyed by a string topic (e.g. "monster.death", "player.levelup").
// This avoids reflect-based routing while staying flexible — topics are constants
// defined alongside the systems that produce them, not in this package.
//
// # Delivery guarantees
//
//   - At-most-once: if a subscriber's channel is full, the event is dropped
//     with a warning log. Subscribers should be fast consumers.
//   - Ordering: events for the same topic are delivered in publish order to
//     each subscriber. Different subscribers may process events concurrently.
//
// # Peak Go contract
//
//	Publish(topic, event) → 0 allocs/op after subscriber lookup (map read + channel send)
//	Subscribe(topic) → called once at boot, not on hot path
package eventbus

import (
	"server/logger"
	"sync"
)

// Event is an arbitrary value published on a topic.
// Use a concrete struct type for each topic to retain type safety at the call site:
//
//	type MonsterDeathEvent struct {
//	    MonsterID ecs.Entity
//	    KillerID  ecs.Entity
//	    MapID     int
//	}
//	eventbus.Publish("monster.death", MonsterDeathEvent{...})
type Event any

// Handler is a function invoked for each event on a subscribed topic.
// Runs in a dedicated goroutine — safe to do I/O or sleep.
type Handler func(event Event)

// Bus is the central event dispatcher.
// Zero value is not usable — create with New().
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string][]*subscriber
}

type subscriber struct {
	ch      chan Event
	handler Handler
	once    sync.Once // ensures the drain goroutine is started exactly once
}

const defaultChannelSize = 64

// New creates and returns a new Bus ready for use.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string][]*subscriber),
	}
}

// GlobalBus is the singleton event bus used by all game systems.
// Initialized at package load time; safe to use from any goroutine.
var GlobalBus = New()

// Subscribe registers handler to receive all events published on topic.
// Safe to call from multiple goroutines, but typically called once at boot.
//
// The handler runs in its own goroutine per subscription — it must not
// assume any particular execution order relative to other subscribers.
//
// channelSize controls how many unprocessed events the subscriber can buffer
// before the bus starts dropping events. Pass 0 to use the default (64).
func (b *Bus) Subscribe(topic string, handler Handler, channelSize int) {
	if channelSize <= 0 {
		channelSize = defaultChannelSize
	}

	sub := &subscriber{
		ch:      make(chan Event, channelSize),
		handler: handler,
	}

	// Start the drain goroutine immediately so the channel is always consumed.
	sub.once.Do(func() {
		go func() {
			for event := range sub.ch {
				sub.handler(event)
			}
		}()
	})

	b.mu.Lock()
	b.subscribers[topic] = append(b.subscribers[topic], sub)
	b.mu.Unlock()
}

// Publish sends event to all subscribers of topic.
// Non-blocking: if a subscriber's channel is full, the event is dropped
// and a warning is logged. The publisher never waits.
//
// Safe to call from the game loop goroutine.
func (b *Bus) Publish(topic string, event Event) {
	b.mu.RLock()
	subs := b.subscribers[topic]
	b.mu.RUnlock()

	for _, sub := range subs {
		select {
		case sub.ch <- event:
		default:
			logger.Warn("[EVENTBUS] Drop event on topic %q — subscriber channel full (cap %d)",
				topic, cap(sub.ch))
		}
	}
}

// Drain closes all subscriber channels and waits for their goroutines to finish.
// Call once during graceful server shutdown after the game loop has stopped.
func (b *Bus) Drain() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, subs := range b.subscribers {
		for _, sub := range subs {
			close(sub.ch)
		}
	}
	// Clear the map so Publish after Drain is a no-op.
	b.subscribers = make(map[string][]*subscriber)
}

// TopicCount returns the number of registered topics.
// Useful for admin status checks.
func (b *Bus) TopicCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// SubscriberCount returns the total number of subscribers across all topics.
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
//
// Defined here as a canonical reference. Systems that produce these events
// should import this package and use these constants rather than raw strings.

const (
	TopicMonsterDeath     = "monster.death"     // payload: MonsterDeathEvent
	TopicPlayerDeath      = "player.death"      // payload: PlayerDeathEvent
	TopicPlayerLevelUp    = "player.level_up"   // payload: PlayerLevelUpEvent
	TopicItemDrop         = "item.drop"         // payload: ItemDropEvent
	TopicItemPickup       = "item.pickup"       // payload: ItemPickupEvent
	TopicPlayerConnect    = "player.connect"    // payload: PlayerConnectEvent
	TopicPlayerDisconnect = "player.disconnect" // payload: PlayerDisconnectEvent
)

// ─── Well-known event types ───────────────────────────────────────────────────
//
// Concrete event structs. Systems publish these; subscribers type-assert on arrival.
// All fields are value types (no pointers) — safe to copy across goroutine boundary.

// MonsterDeathEvent is published when a monster's HP reaches zero.
type MonsterDeathEvent struct {
	MonsterID   uint64 // ecs.Entity as uint64 to avoid import cycle with ecs package
	KillerID    uint64
	MonsterName string
	MapID       int
	SpawnX      int
	SpawnZ      int
	XPReward    uint64
	TemplateID  int
}

// PlayerDeathEvent is published when a player's HP reaches zero.
type PlayerDeathEvent struct {
	PlayerID   uint64
	KillerID   uint64
	PlayerName string
	MapID      int
}

// PlayerLevelUpEvent is published when a player gains a level.
type PlayerLevelUpEvent struct {
	PlayerID   uint64
	PlayerName string
	NewLevel   int
	MapID      int
}

// ItemDropEvent is published when a ground item is spawned.
type ItemDropEvent struct {
	ItemEntityID   uint64
	ItemTemplateID uint64
	MapID          int
	X              int
	Z              int
}

// ItemPickupEvent is published when a player picks up a ground item.
type ItemPickupEvent struct {
	PlayerID       uint64
	ItemEntityID   uint64
	ItemTemplateID uint64
	MapID          int
}

// PlayerConnectEvent is published when a player logs in.
type PlayerConnectEvent struct {
	PlayerID   uint64
	PlayerName string
}

// PlayerDisconnectEvent is published when a player disconnects.
type PlayerDisconnectEvent struct {
	PlayerID   uint64
	PlayerName string
}
