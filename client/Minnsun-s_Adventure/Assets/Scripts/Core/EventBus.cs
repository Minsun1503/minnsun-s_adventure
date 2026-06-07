using System;
using System.Collections.Generic;
using UnityEngine;

/// <summary>
/// Lightweight main-thread event bus for decoupled pub/sub communication.
///
/// Generic on event type T (must be a struct to avoid GC pressure).
/// Subscribers are called during MonoBehaviour.Update() on the main thread,
/// ensuring thread safety for Unity API calls.
///
/// Usage:
///   // Define event struct
///   public struct PlayerDiedEvent { public ulong EntityID; }
///
///   // Subscribe (anywhere)
///   EventBus<PlayerDiedEvent>.Subscribe(OnPlayerDied);
///
///   // Publish (from any thread)
///   EventBus<PlayerDiedEvent>.Publish(new PlayerDiedEvent { EntityID = id });
/// </summary>
public static class EventBus<T> where T : struct
{
    private static readonly List<Action<T>> Subscribers = new List<Action<T>>(4);
    private static readonly Queue<T> PendingQueue = new Queue<T>(16);
    private static readonly object QueueLock = new object();

    /// <summary>
    /// Register a handler. Called during EventBus.FlushAll() on main thread.
    /// </summary>
    public static void Subscribe(Action<T> handler)
    {
        if (handler == null) return;

        lock (Subscribers)
        {
            if (!Subscribers.Contains(handler))
                Subscribers.Add(handler);
        }
    }

    /// <summary>
    /// Unregister a previously subscribed handler.
    /// </summary>
    public static void Unsubscribe(Action<T> handler)
    {
        if (handler == null) return;

        lock (Subscribers)
        {
            Subscribers.Remove(handler);
        }
    }

    /// <summary>
    /// Enqueue an event for main-thread dispatch.
    /// Thread-safe — can be called from any thread.
    /// </summary>
    public static void Publish(T eventData)
    {
        lock (QueueLock)
        {
            PendingQueue.Enqueue(eventData);
        }
    }

    /// <summary>
    /// Clear all pending events without dispatching them.
    /// </summary>
    public static void ClearPending()
    {
        lock (QueueLock)
        {
            PendingQueue.Clear();
        }
    }

    /// <summary>
    /// Remove all subscribers and clear pending events.
    /// </summary>
    public static void Reset()
    {
        lock (Subscribers)
        {
            Subscribers.Clear();
        }
        ClearPending();
    }

    /// <summary>
    /// Flush all pending events to subscribers on the current thread.
    /// Call this from MonoBehaviour.Update() or a dedicated dispatcher.
    /// </summary>
    public static void Flush()
    {
        // Snapshot pending queue outside lock to minimize contention
        T[] batch;
        int count;
        lock (QueueLock)
        {
            count = PendingQueue.Count;
            if (count == 0) return;
            batch = new T[count];
            for (int i = 0; i < count; i++)
                batch[i] = PendingQueue.Dequeue();
        }

        // Snapshot subscribers inside lock
        Action<T>[] handlers;
        lock (Subscribers)
        {
            if (Subscribers.Count == 0) return;
            handlers = Subscribers.ToArray();
        }

        // Dispatch
        for (int i = 0; i < handlers.Length; i++)
        {
            for (int j = 0; j < batch.Length; j++)
            {
                try
                {
                    handlers[i](batch[j]);
                }
                catch (Exception ex)
                {
                    Debug.LogError($"[EventBus<{typeof(T).Name}>] Handler error: {ex.Message}");
                }
            }
        }
    }
}

/// <summary>
/// Global event bus dispatcher — attach this to a persistent GameObject
/// and it will flush all EventBus<T> instances in Update().
///
/// Must register all event types used in the project.
/// </summary>
public class EventBusDispatcher : MonoBehaviour
{
    // ─── Register all event bus types here ──────────────────────────────
    // Each delegate is called once per frame to flush that event type.
    private readonly List<Action> flushers = new List<Action>();

    // Built-in game events — add more as the project grows
    public struct PlayerLoginEvent      { public ulong EntityID; public string Username; }
    public struct PlayerMoveEvent       { public ulong EntityID; public int X; public int Z; }
    public struct EntitySpawnedEvent    { public ulong EntityID; public byte Type; public int MapID; public int X; public int Z; public string Name; }
    public struct EntityDespawnedEvent  { public ulong EntityID; }
    public struct CombatHitEvent        { public ulong AttackerID; public ulong TargetID; public int Damage; public byte Killed; }
    public struct StatsUpdatedEvent     { public ulong EntityID; }
    public struct ChatMessageEvent      { public byte Channel; public string Sender; public string Message; }
    public struct NoticeEvent           { public string Message; }
    public struct ConnectionEvent       { public bool Connected; }

    private void Awake()
    {
        DontDestroyOnLoad(gameObject);

        // Register all event bus flushers
        flushers.Add(EventBus<PlayerLoginEvent>.Flush);
        flushers.Add(EventBus<PlayerMoveEvent>.Flush);
        flushers.Add(EventBus<EntitySpawnedEvent>.Flush);
        flushers.Add(EventBus<EntityDespawnedEvent>.Flush);
        flushers.Add(EventBus<CombatHitEvent>.Flush);
        flushers.Add(EventBus<StatsUpdatedEvent>.Flush);
        flushers.Add(EventBus<ChatMessageEvent>.Flush);
        flushers.Add(EventBus<NoticeEvent>.Flush);
        flushers.Add(EventBus<ConnectionEvent>.Flush);
    }

    private void Update()
    {
        for (int i = 0; i < flushers.Count; i++)
        {
            try { flushers[i](); }
            catch (Exception ex) { Debug.LogError($"[EventBusDispatcher] Flush error: {ex.Message}"); }
        }
    }
}