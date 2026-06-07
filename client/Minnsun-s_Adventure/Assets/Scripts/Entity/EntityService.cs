using System.Collections;
using UnityEngine;

/// <summary>
/// MonoBehaviour that orchestrates entity lifecycle:
/// - Listens to EventBus events (Spawn, Despawn, Move, Stats, Combat)
/// - Creates/destroys entity GameObjects with appropriate View components
/// - Delegates stats/combat updates to the corresponding EntityView
///
/// Must be attached to a persistent GameObject (the Bootstrap root).
/// </summary>
public class EntityService : MonoBehaviour
{
    private EntityRegistry registry;
    private UIManager uiManager;
    private Transform entityRoot;

    /// <summary>
    /// Initialize with references. Called from GameBootstrap after creating EntityService.
    /// </summary>
    public void Init(EntityRegistry reg, UIManager ui, Transform root)
    {
        registry = reg;
        uiManager = ui;
        entityRoot = root;
    }

    private void Start()
    {
        // Register EventBus subscribers
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Subscribe(OnEntitySpawned);
        EventBus<EventBusDispatcher.EntityDespawnedEvent>.Subscribe(OnEntityDespawned);
        EventBus<EventBusDispatcher.PlayerMoveEvent>.Subscribe(OnPlayerMove);
        EventBus<EventBusDispatcher.StatsUpdatedEvent>.Subscribe(OnStatsUpdated);
        EventBus<EventBusDispatcher.CombatHitEvent>.Subscribe(OnCombatHit);

        // If registry wasn't injected via Init(), try resolving from ServiceContainer
        if (registry == null)
            registry = ServiceContainer.Resolve<EntityRegistry>();

        Logger.D("EntityService", "EventBus subscribers registered");
    }

    private void OnDestroy()
    {
        // Unsubscribe all
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Unsubscribe(OnEntitySpawned);
        EventBus<EventBusDispatcher.EntityDespawnedEvent>.Unsubscribe(OnEntityDespawned);
        EventBus<EventBusDispatcher.PlayerMoveEvent>.Unsubscribe(OnPlayerMove);
        EventBus<EventBusDispatcher.StatsUpdatedEvent>.Unsubscribe(OnStatsUpdated);
        EventBus<EventBusDispatcher.CombatHitEvent>.Unsubscribe(OnCombatHit);

        Logger.D("EntityService", "EventBus subscribers unregistered");
    }

    // ─── Event Handlers ──────────────────────────────────────────────────

    /// <summary>
    /// Handle EntitySpawnedEvent — create a GameObject with the appropriate View.
    /// </summary>
    private void OnEntitySpawned(EventBusDispatcher.EntitySpawnedEvent evt)
    {
        if (registry == null) return;

        // Prevent double-spawn
        if (registry.Get(evt.EntityID) != null)
        {
            Logger.W("EntityService", "Duplicate spawn for EntityID={0} — ignored", evt.EntityID);
            return;
        }

        // Determine primitive type, color, and view component based on entity type
        PrimitiveType primitiveType;
        Color color;
        System.Type viewType;

        switch (evt.Type)
        {
            case 0: // Player
                primitiveType = PrimitiveType.Capsule;
                color = Color.blue;
                viewType = typeof(PlayerView);
                break;
            case 1: // Monster
                primitiveType = PrimitiveType.Cube;
                color = Color.red;
                viewType = typeof(MonsterView);
                break;
            default: // Ground item, etc.
                primitiveType = PrimitiveType.Sphere;
                color = Color.yellow;
                viewType = typeof(EntityView);
                break;
        }

        // Create primitive with name
        string entityName = string.IsNullOrEmpty(evt.Name) ? $"Entity_{evt.EntityID}" : evt.Name;
        GameObject go = GameObject.CreatePrimitive(primitiveType);
        go.name = entityName;
        go.transform.SetParent(entityRoot);
        // Set initial position
        go.transform.position = new Vector3(evt.X, 0f, evt.Z);

        // Set color
        Renderer renderer = go.GetComponent<Renderer>();
        if (renderer != null)
            renderer.material.color = color;

        // Attach correct view component
        EntityView view = (EntityView)go.AddComponent(viewType);
        view.EntityID = evt.EntityID;
        view.EntityName = entityName;
        view.EntityType = evt.Type;
        view.SetTargetPosition(evt.X, evt.Z); // also set for lerping

        // Register in registry
        registry.Add(evt.EntityID, view);

        Logger.D("EntityService", "Spawned {0} Type={1} ID={2} at ({3}, {4})", entityName, evt.Type, evt.EntityID, evt.X, evt.Z);

        // If this is a PlayerView, check if it's the local player
        if (view is PlayerView playerView)
        {
            playerView.InitAsLocalPlayer(registry.LocalPlayerID);
        }
    }

    /// <summary>
    /// Handle EntityDespawnedEvent — destroy the GameObject and remove from registry.
    /// </summary>
    private void OnEntityDespawned(EventBusDispatcher.EntityDespawnedEvent evt)
    {
        if (registry == null) return;

        EntityView view = registry.Get(evt.EntityID);
        if (view == null)
        {
            Logger.W("EntityService", "Despawn unknown EntityID={0}", evt.EntityID);
            return;
        }

        registry.Remove(evt.EntityID);
        Destroy(view.gameObject);
        Logger.D("EntityService", "Despawned EntityID={0}", evt.EntityID);
    }

    /// <summary>
    /// Handle PlayerMoveEvent — update the entity's target position for Lerp.
    /// </summary>
    private void OnPlayerMove(EventBusDispatcher.PlayerMoveEvent evt)
    {
        if (registry == null) return;

        EntityView view = registry.Get(evt.EntityID);
        if (view == null)
        {
            Logger.W("EntityService", "Position update for unknown EntityID={0}", evt.EntityID);
            return;
        }

        view.SetTargetPosition(evt.X, evt.Z);
    }

    /// <summary>
    /// Handle StatsUpdatedEvent — update entity stats and refresh HUD if local player.
    /// </summary>
    private void OnStatsUpdated(EventBusDispatcher.StatsUpdatedEvent evt)
    {
        if (registry == null) return;

        EntityView view = registry.Get(evt.EntityID);
        if (view == null)
        {
            Logger.W("EntityService", "Stats update for unknown EntityID={0}", evt.EntityID);
            return;
        }

        // Stats values are already decoded and stored in the event.
        // For simplicity, we use the event's existing data path.
        // The actual HP/MP/Dam/Level values come via direct method call from PacketRouter.
        Logger.D("EntityService", "Stats updated for EntityID={0}", evt.EntityID);

        // Notify UIManager if this is the local player
        if (evt.EntityID == registry.LocalPlayerID && uiManager != null)
        {
            // UIManager also listens to EventBus, so this may be redundant.
            // But keep direct call for immediate HUD update.
        }
    }

    /// <summary>
    /// Handle CombatHitEvent — apply damage, flash monster, show damage number.
    /// If Killed==1, despawn after a short delay.
    /// </summary>
    private void OnCombatHit(EventBusDispatcher.CombatHitEvent evt)
    {
        if (registry == null) return;

        // Update target HP
        EntityView targetView = registry.Get(evt.TargetID);
        if (targetView != null)
        {
            targetView.ApplyDamage(evt.Damage);

            // Flash monster white on hit
            if (targetView is MonsterView monsterView)
            {
                monsterView.FlashHit();
            }
        }

        // Show damage number via UIManager
        if (uiManager != null)
        {
            // Build a Decoders.CombatHitPacket from the event for UIManager compatibility
            var combatPacket = new Decoders.CombatHitPacket
            {
                AttackerID = evt.AttackerID,
                TargetID = evt.TargetID,
                Damage = evt.Damage,
                TargetHP = targetView != null ? targetView.HP : 0,
                Killed = evt.Killed
            };
            uiManager.ShowDamageNumber(combatPacket);
        }

        // If killed, despawn after delay
        if (evt.Killed == 1)
        {
            StartCoroutine(DespawnAfterDelay(evt.TargetID, 1.5f));
        }
    }

    private IEnumerator DespawnAfterDelay(ulong entityID, float delay)
    {
        yield return new WaitForSeconds(delay);

        if (registry == null) yield break;

        EntityView view = registry.Get(entityID);
        if (view == null) yield break;

        registry.Remove(entityID);
        Destroy(view.gameObject);
        Logger.D("EntityService", "Despawned (killed) EntityID={0} after {1}s delay", entityID, delay);
    }
}