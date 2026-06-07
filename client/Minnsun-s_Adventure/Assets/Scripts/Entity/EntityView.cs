using UnityEngine;

/// <summary>
/// Base class for all entity GameObjects.
/// Handles position interpolation (Lerp) toward targetPosition in Update().
/// Stores stats inline (HP, MP, Dam, Level) — no separate EntityStats component needed.
/// </summary>
public class EntityView : MonoBehaviour
{
    // ─── Identity ─────────────────────────────────────────────────────────
    public ulong EntityID { get; set; }
    public string EntityName { get; set; }
    public byte EntityType { get; set; } // 0=Player, 1=Monster, 2=GroundItem

    // ─── Movement ─────────────────────────────────────────────────────────
    /// <summary>The target world position received from server PositionSync.</summary>
    public Vector3 TargetPosition { get; set; }

    /// <summary>Lerp speed factor. Higher = faster convergence.</summary>
    public float LerpSpeed { get; set; } = 10f;

    // ─── Stats (inline, no separate component) ───────────────────────────
    public int HP { get; set; }
    public int MaxHP { get; set; }
    public int MP { get; set; }
    public int MaxMP { get; set; }
    public int Dam { get; set; }
    public int Level { get; set; }

    // ─── Unity Lifecycle ─────────────────────────────────────────────────

    protected virtual void Awake()
    {
        // Set initial target position to spawn position
        TargetPosition = transform.position;
    }

    protected virtual void Update()
    {
        // Lerp toward target position for smooth movement
        if (Vector3.Distance(transform.position, TargetPosition) > 0.001f)
        {
            transform.position = Vector3.Lerp(
                transform.position,
                TargetPosition,
                LerpSpeed * Time.deltaTime
            );
        }
        else if (transform.position != TargetPosition)
        {
            // Snap to exact target if very close (avoid micro-oscillation)
            transform.position = TargetPosition;
        }
    }

    // ─── Public API ──────────────────────────────────────────────────────

    /// <summary>Update target position from a server sync packet.</summary>
    public void SetTargetPosition(float x, float z)
    {
        TargetPosition = new Vector3(x, 0, z);
    }

    /// <summary>Update stats from a StatsSync packet.</summary>
    public void UpdateStats(int hp, int maxHp, int mp, int maxMp, int dam, int level)
    {
        HP = hp;
        MaxHP = maxHp;
        MP = mp;
        MaxMP = maxMp;
        Dam = dam;
        Level = level;
    }

    /// <summary>Apply damage and return remaining HP.</summary>
    public int ApplyDamage(int damage)
    {
        HP = Mathf.Max(0, HP - damage);
        return HP;
    }
}