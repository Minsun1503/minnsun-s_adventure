using System;
using UnityEngine;

/// <summary>
/// Platform switcher that enables the correct network client for each build target.
///
/// - WebGL builds: Use NetworkClientWS (NativeWebSocket)
/// - All other platforms (PC, Android, etc.): Use NetworkClient (TCP)
///
/// Attach this to the same persistent GameObject as NetworkClient and NetworkClientWS.
/// It will enable exactly one transport based on the platform.
/// </summary>
public class NetworkManager : MonoBehaviour
{
    /// <summary>Fired once when the currently active transport connects (on Unity main thread).</summary>
    public event Action OnConnected;

    /// <summary>Fired when the currently active transport disconnects (on Unity main thread).</summary>
    public event Action OnDisconnected;

    private void Awake()
    {
        // Ensure we don't get destroyed on scene load — must persist across all scenes.
        DontDestroyOnLoad(gameObject);

        NetworkClient tcpClient = GetComponent<NetworkClient>();
        NetworkClientWS wsClient = GetComponent<NetworkClientWS>();

#if UNITY_WEBGL && !UNITY_EDITOR
        // WebGL build: use WebSocket transport
        if (wsClient != null) wsClient.enabled = true;
        if (tcpClient != null) tcpClient.enabled = false;
        Debug.Log("[NET] Platform: WebGL — using WebSocket transport");
#else
        // All other platforms: use TCP transport
        if (tcpClient != null) tcpClient.enabled = true;
        if (wsClient != null) wsClient.enabled = false;
        Debug.Log($"[NET] Platform: {(Application.isEditor ? "Editor" : Application.platform.ToString())} — using TCP transport");
#endif

        // Forward the active transport's events to our own events.
        if (tcpClient != null)
        {
            tcpClient.OnConnected += () => OnConnected?.Invoke();
            tcpClient.OnDisconnected += () => OnDisconnected?.Invoke();
        }
        if (wsClient != null)
        {
            wsClient.OnConnected += () => OnConnected?.Invoke();
            wsClient.OnDisconnected += () => OnDisconnected?.Invoke();
        }
    }

    /// <summary>
    /// Get the latest ping from the currently active transport.
    /// </summary>
    public float LastPingMs
    {
        get
        {
#if UNITY_WEBGL && !UNITY_EDITOR
            var ws = GetComponent<NetworkClientWS>();
            if (ws != null && ws.enabled) return ws.LastPingMs;
#else
            var tcp = GetComponent<NetworkClient>();
            if (tcp != null && tcp.enabled) return tcp.LastPingMs;
#endif
            return 0f;
        }
    }

    /// <summary>
    /// Send a binary packet using the currently active transport.
    /// Convenience method so game code doesn't need to check the platform.
    /// </summary>
    public void SendPacket(byte opcode, byte[] payload)
    {
#if UNITY_WEBGL && !UNITY_EDITOR
        var ws = GetComponent<NetworkClientWS>();
        if (ws != null && ws.enabled)
        {
            ws.SendPacket(opcode, payload);
        }
#else
        var tcp = GetComponent<NetworkClient>();
        if (tcp != null && tcp.enabled)
        {
            tcp.SendPacket(opcode, payload);
        }
#endif
    }

    /// <summary>
    /// Start heartbeat on the currently active transport.
    /// Call this after login success.
    /// </summary>
    public void StartHeartbeat()
    {
#if UNITY_WEBGL && !UNITY_EDITOR
        var ws = GetComponent<NetworkClientWS>();
        if (ws != null && ws.enabled)
        {
            ws.StartHeartbeat();
        }
#else
        var tcp = GetComponent<NetworkClient>();
        if (tcp != null && tcp.enabled)
        {
            tcp.StartHeartbeat();
        }
#endif
    }
}