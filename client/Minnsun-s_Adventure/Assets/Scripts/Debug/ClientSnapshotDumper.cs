using UnityEngine;

/// <summary>
/// Static debug snapshot dumper — collects client state into a JSON log line
/// and sends it as a WebSocket text frame to the server trace log.
///
/// The server's WSConn.Read() intercepts [SNAPSHOT]-prefixed text frames and
/// writes them to the JSONL trace file (trace-YYYY-MM-DD.jsonl) via
/// logger.PushTraceLog() with "source": "client".
///
/// Falls back to Debug.Log if no NetworkClientWS instance is available.
///
/// Usage:
///   ClientSnapshotDumper.Dump(traceId, reason);
/// </summary>
public static class ClientSnapshotDumper
{
    // Cached reference to avoid repeated FindObjectOfType calls
    private static NetworkClientWS _ws;
    private static bool _wsSearched;

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

        // ── Output: WebSocket text frame (preferred) or Debug.Log ────
        SendSnapshot(json);
    }

    /// <summary>
    /// Send the snapshot JSON as a WebSocket text frame to the server's
    /// trace log. Falls back to Debug.Log if no NetworkClientWS found.
    /// </summary>
    private static void SendSnapshot(string json)
    {
        // Locate NetworkClientWS (lazy cached)
        if (!_wsSearched)
        {
            _ws = Object.FindFirstObjectByType<NetworkClientWS>();
            _wsSearched = true;
        }

        if (_ws != null && _ws.IsConnected)
        {
            _ws.SendTextFrame("[SNAPSHOT] " + json);
        }
        else
        {
            // Fallback: just log to Unity console
            Debug.Log("[SNAPSHOT] " + json);
        }
    }
}