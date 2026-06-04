using System.Collections.Generic;
using UnityEngine;

/// <summary>
/// Manages all spawned entities in the game world.
/// Dictionary<ulong, GameObject> maps server EntityID → Unity GameObject.
/// All GameObject creation is done in code (no prefabs).
/// </summary>
public class EntityManager : MonoBehaviour
{
    /// <summary>All active entities keyed by server EntityID.</summary>
    public Dictionary<ulong, GameObject> Entities { get; private set; } = new Dictionary<ulong, GameObject>();

    /// <summary>The local player's EntityID — set when first StatsSync arrives after login.</summary>
    public ulong LocalPlayerID { get; set; } = 0;

    /// <summary>Reference to the root transform that parents all entity GameObjects.</summary>
    public Transform EntityRoot { get; set; }

    // ─── Spawn / Despawn ─────────────────────────────────────────────

    /// <summary>
    /// Spawn a new entity GameObject. Tags: "Player" / "Monster" / "Item".
    /// If EntityID == LocalPlayerID, attaches a PlayerController component.
    /// Temporary visual: colored cube primitive (swap with real assets later).
    /// </summary>
    public void Spawn(Decoders.SpawnPacket packet)
    {
        if (Entities.ContainsKey(packet.EntityID))
        {
            Debug.LogWarning($"[EntityMgr] Duplicate spawn for EntityID={packet.EntityID}");
            return;
        }

        string tag;
        Color color;
        switch (packet.Type)
        {
            case 0: // Player
                tag = "Player";
                color = Color.blue;
                break;
            case 1: // Monster
                tag = "Monster";
                color = Color.red;
                break;
            default: // Ground item, etc.
                tag = "Item";
                color = Color.yellow;
                break;
        }

        // Create primitive cube as placeholder
        GameObject go = GameObject.CreatePrimitive(PrimitiveType.Cube);
        go.name = packet.Name ?? $"Entity_{packet.EntityID}";
        go.tag = tag;
        go.transform.SetParent(EntityRoot);
        go.transform.position = new Vector3(packet.X, 0, packet.Z);

        // Set a basic material color so different entity types are distinguishable
        Renderer renderer = go.GetComponent<Renderer>();
        if (renderer != null)
            renderer.material.color = color;

        // Attach PlayerController if this is the local player
        if (packet.EntityID == LocalPlayerID)
        {
            go.AddComponent<PlayerController>();
            Debug.Log($"[EntityMgr] Local player spawned: {go.name} (ID={packet.EntityID})");
        }

        Entities[packet.EntityID] = go;
    }

    /// <summary>Despawn (destroy) an entity by its server EntityID.</summary>
    public void Despawn(ulong entityID)
    {
        if (!Entities.TryGetValue(entityID, out GameObject go))
        {
            Debug.LogWarning($"[EntityMgr] Despawn unknown EntityID={entityID}");
            return;
        }

        Entities.Remove(entityID);
        Destroy(go);
    }

    // ─── Updates ────────────────────────────────────────────────────

    /// <summary>Update an entity's world position (immediate snap, no lerp yet).</summary>
    public void UpdatePosition(Decoders.PositionPacket packet)
    {
        if (!Entities.TryGetValue(packet.EntityID, out GameObject go))
        {
            Debug.LogWarning($"[EntityMgr] Position update for unknown EntityID={packet.EntityID}");
            return;
        }

        go.transform.position = new Vector3(packet.X, 0, packet.Z);
    }

    /// <summary>
    /// Update an entity's stats. If this is the local player, notify UIManager to refresh the HUD.
    /// </summary>
    public void UpdateStats(Decoders.StatsPacket packet)
    {
        if (!Entities.TryGetValue(packet.EntityID, out GameObject go))
        {
            Debug.LogWarning($"[EntityMgr] Stats update for unknown EntityID={packet.EntityID}");
            return;
        }

        // Store stats on the GameObject for other systems to read
        var stats = go.GetComponent<EntityStats>();
        if (stats == null)
            stats = go.AddComponent<EntityStats>();

        stats.HP    = packet.HP;
        stats.MaxHP = packet.MaxHP;
        stats.MP    = packet.MP;
        stats.MaxMP = packet.MaxMP;
        stats.Dam   = packet.Dam;
        stats.Level = packet.Level;

        // If this is the local player, set LocalPlayerID (first time)
        if (LocalPlayerID == 0)
        {
            LocalPlayerID = packet.EntityID;
            Debug.Log($"[EntityMgr] LocalPlayerID set to {packet.EntityID}");
        }

        // Notify HUD if this is the local player
        if (packet.EntityID == LocalPlayerID)
        {
            var ui = FindObjectOfType<UIManager>();
            if (ui != null)
                ui.UpdateHUD(packet);
        }
    }

    /// <summary>Update the target entity's HP after a combat hit. If Killed==1, trigger death.</summary>
    public void UpdateHP(Decoders.CombatHitPacket packet)
    {
        if (!Entities.TryGetValue(packet.TargetID, out GameObject go))
        {
            Debug.LogWarning($"[EntityMgr] HP update for unknown EntityID={packet.TargetID}");
            return;
        }

        var stats = go.GetComponent<EntityStats>();
        if (stats != null)
            stats.HP = packet.TargetHP;

        // If killed, despawn after a short delay
        if (packet.Killed == 1)
        {
            StartCoroutine(DespawnDelayed(packet.TargetID, 1.5f));
        }
    }

    private System.Collections.IEnumerator DespawnDelayed(ulong entityID, float delay)
    {
        yield return new WaitForSeconds(delay);
        Despawn(entityID);
    }
}

/// <summary>
/// Simple component attached to every entity GameObject to expose runtime stats.
/// </summary>
public class EntityStats : MonoBehaviour
{
    public int HP;
    public int MaxHP;
    public int MP;
    public int MaxMP;
    public int Dam;
    public int Level;
}