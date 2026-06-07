using UnityEngine;

/// <summary>
/// Monster entity view — red Cube.
/// Manages basic combat state visual feedback.
/// </summary>
public class MonsterView : EntityView
{
    /// <summary>Renderer cached for color flashes on hit.</summary>
    private Renderer cachedRenderer;
    private Color originalColor;

    protected override void Awake()
    {
        base.Awake();
        cachedRenderer = GetComponent<Renderer>();
        if (cachedRenderer != null)
            originalColor = cachedRenderer.material.color;
    }

    /// <summary>
    /// Flash red briefly when hit, then restore original color after 0.2s.
    /// Called from EntityService on CombatHitEvent.
    /// </summary>
    public void FlashHit()
    {
        if (cachedRenderer == null) return;

        cachedRenderer.material.color = Color.white;
        Invoke(nameof(RestoreColor), 0.2f);
    }

    private void RestoreColor()
    {
        if (cachedRenderer != null)
            cachedRenderer.material.color = originalColor;
    }
}