using UnityEngine;

/// <summary>
/// Procedural target ring rendered via LineRenderer.
/// Draws a yellow circle on the ground beneath a selected entity.
/// Attached to a child GameObject of PlayerController; activated/deactivated on target selection.
///
/// Uses 36 segments for smooth circle approximation.
/// Ring sits at RingHeight above ground (0.05f) to avoid z-fighting.
/// </summary>
public class TargetIndicator : MonoBehaviour
{
    private LineRenderer line;

    /// <summary>Number of circle segments. 36 ≈ 10° per segment.</summary>
    private const int Segments = 36;

    /// <summary>Radius of the target ring in world units.</summary>
    private const float Radius = 0.8f;

    /// <summary>Height offset above ground to avoid z-fighting.</summary>
    private const float RingHeight = 0.05f;

    // ─── Lifecycle ───────────────────────────────────────────────────────

    private void Awake()
    {
        // Add LineRenderer and configure
        line = gameObject.AddComponent<LineRenderer>();
        line.positionCount = Segments + 1; // +1 to close the loop
        line.loop = false;

        // Thin yellow line
        line.startWidth = 0.06f;
        line.endWidth = 0.06f;

        // Use Sprites/Default shader for simple color rendering
        line.material = new Material(Shader.Find("Sprites/Default"));
        line.startColor = Color.yellow;
        line.endColor = Color.yellow;

        // Draw the procedural circle
        Redraw();
    }

    // ─── Public API ──────────────────────────────────────────────────────

    /// <summary>
    /// Rebuild the circle vertices (call if Radius or Segments change at runtime).
    /// </summary>
    public void Redraw()
    {
        if (line == null) return;

        float angleStep = 360f / Segments;
        Vector3[] positions = new Vector3[Segments + 1];

        for (int i = 0; i <= Segments; i++)
        {
            float angle = Mathf.Deg2Rad * (angleStep * i);
            float x = Mathf.Cos(angle) * Radius;
            float z = Mathf.Sin(angle) * Radius;
            positions[i] = new Vector3(x, RingHeight, z);
        }

        line.SetPositions(positions);
    }

    /// <summary>
    /// Set the ring color (e.g., yellow for selected, red for hostile).
    /// </summary>
    public void SetColor(Color color)
    {
        if (line != null)
        {
            line.startColor = color;
            line.endColor = color;
        }
    }
}