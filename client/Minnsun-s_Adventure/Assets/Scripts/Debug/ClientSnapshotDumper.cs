using UnityEngine;

/// <summary>
/// Static debug snapshot dumper — collects client state into a JSON log line
/// so the MCP console bridge can read it and correlate with server logs.
///
/// Usage:
///   ClientSnapshotDumper.Dump(traceId, reason);
///
/// WebGL: outputs via Debug.Log("[SNAPSHOT] " + json)
/// </summary>
public static class ClientSnapshotDumper
{
    /// <summary>
    /// Collect and log a diagnostic snapshot of the current client state.
    /// Safe to call from any MonoBehaviour Update() or event handler (main thread only).
    /// </summary>
    /// <param name="traceId">4-byte hex trace ID from C2S packet (or empty string).</param>
    /// <param name="reason">Why this dump was triggered (e.g. "Error_7", "F9_Manual").</param>
    public static void Dump(string traceId, string reason)
    {
        // ── Timestamp ─────────────────────────────────────────────────
        string timestamp = System.DateTime.UtcNow.ToString("yyyy-MM-ddTHH:mm:ss.fffZ");

        // ── EntityRegistry: local player stats + position ─────────────
        ulong localPlayerID = 0;
        int hp = 0, mp = 0, level = 0;
        float posX = 0f, posZ = 0f;

        var registry = ServiceContainer.Resolve<EntityRegistry>();
        if (registry != null)
        {
            localPlayerID = registry.LocalPlayerID;
            EntityView localView = registry.Get(localPlayerID);
            if (localView != null)
            {
                hp = localView.HP;
                mp = localView.MP;
                level = localView.Level;
                Vector3 pos = localView.transform.position;
                posX = pos.x;
                posZ = pos.z;
            }
        }

        // ── VirtualJoystick ───────────────────────────────────────────
        float joyX = 0f, joyY = 0f;
        bool attackPressed = false;

        if (VirtualJoystick.Instance != null)
        {
            Vector2 dir = VirtualJoystick.Instance.InputDirection;
            joyX = dir.x;
            joyY = dir.y;
            attackPressed = VirtualJoystick.Instance.IsAttackPressed;
        }

        // ── NetworkManager: ping ──────────────────────────────────────
        float pingMs = 0f;
        var netManager = ServiceContainer.Resolve<NetworkManager>();
        if (netManager != null)
            pingMs = netManager.LastPingMs;

        // ── PlayerController: selected target ─────────────────────────
        ulong targetID = 0;
        var pc = Object.FindFirstObjectByType<PlayerController>();
        if (pc != null)
            targetID = pc.SelectedTargetID;

        // ── Build JSON manually (lightweight, no alloc for serializer) ─
        // Using string concatenation to avoid JsonUtility overhead
        string json = string.Format(
            "{{" +
            "\"traceId\":\"{0}\"," +
            "\"reason\":\"{1}\"," +
            "\"ts\":\"{2}\"," +
            "\"localPlayerID\":{3}," +
            "\"hp\":{4}," +
            "\"mp\":{5}," +
            "\"level\":{6}," +
            "\"posX\":{7:F1}," +
            "\"posZ\":{8:F1}," +
            "\"joyX\":{9:F3}," +
            "\"joyY\":{10:F3}," +
            "\"attackPressed\":{11}," +
            "\"pingMs\":{12:F0}," +
            "\"targetID\":{13}" +
            "}}",
            traceId ?? "",
            reason ?? "",
            timestamp,
            localPlayerID,
            hp,
            mp,
            level,
            posX,
            posZ,
            joyX,
            joyY,
            attackPressed ? "true" : "false",
            pingMs,
            targetID
        );

        // ── Output ────────────────────────────────────────────────────
        Debug.Log("[SNAPSHOT] " + json);
    }
}