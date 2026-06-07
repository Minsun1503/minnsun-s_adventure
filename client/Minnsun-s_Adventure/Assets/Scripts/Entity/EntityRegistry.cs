using System.Collections.Generic;
using UnityEngine;

/// <summary>
/// Pure C# registry holding all active entity views keyed by server EntityID.
/// Singleton — register one instance in ServiceContainer at bootstrap.
/// Not a MonoBehaviour — zero per-frame overhead.
/// </summary>
public class EntityRegistry
{
    private readonly Dictionary<ulong, EntityView> entities = new Dictionary<ulong, EntityView>(64);

    /// <summary>Server-assigned EntityID of the local player. Set from S2CSuccess.</summary>
    public ulong LocalPlayerID { get; set; }

    // ─── Lifecycle ───────────────────────────────────────────────────────

    /// <summary>Add or overwrite an entity view.</summary>
    public void Add(ulong entityID, EntityView view)
    {
        entities[entityID] = view;
    }

    /// <summary>Remove an entity by ID. Returns true if existed.</summary>
    public bool Remove(ulong entityID)
    {
        return entities.Remove(entityID);
    }

    /// <summary>Get an entity view by ID. Returns null if not found.</summary>
    public EntityView Get(ulong entityID)
    {
        entities.TryGetValue(entityID, out EntityView view);
        return view;
    }

    /// <summary>Get all entity ID → view pairs (for iteration).</summary>
    public IEnumerable<KeyValuePair<ulong, EntityView>> GetAll()
    {
        return entities;
    }

    /// <summary>Get count of active entities.</summary>
    public int Count => entities.Count;

    /// <summary>Clear all entities (on disconnect / scene reload).</summary>
    public void Clear()
    {
        entities.Clear();
    }
}