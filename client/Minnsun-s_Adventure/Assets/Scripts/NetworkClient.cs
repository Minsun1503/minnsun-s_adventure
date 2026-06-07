using System;
using System.Collections;
using System.Collections.Concurrent;
using System.Net.Sockets;
using System.Threading;
using System.Threading.Tasks;
using UnityEngine;
using System.Runtime.CompilerServices;

/// <summary>
/// Minimal main-thread action queue for Unity.
/// Callbacks are flushed in Update() on the Unity main thread.
/// </summary>
public static class UnityMainThreadDispatcher
{
    private static readonly ConcurrentQueue<Action> queue = new ConcurrentQueue<Action>();

    public static void Enqueue(Action action)
    {
        queue.Enqueue(action);
    }

    /// <summary>Call from MonoBehaviour.Update() once per frame.</summary>
    public static void Flush()
    {
        while (queue.TryDequeue(out Action action))
        {
            try { action(); }
            catch (Exception ex) { Debug.LogError($"[Dispatcher] {ex.Message}"); }
        }
    }
}

/// <summary>
/// TCP network client with binary packet framing, heartbeat, auto-reconnect, and main-thread dispatch.
/// Attach to a persistent GameObject; set host/port in Inspector; call StartHeartbeat() after login success.
/// </summary>
public class NetworkClient : MonoBehaviour
{
    [SerializeField] private string serverHost = "127.0.0.1";
    [SerializeField] private int serverPort = 1503;

    public bool IsConnected => connected;

    /// <summary>Fired once when the TCP connection is established (on Unity main thread).</summary>
    public event System.Action OnConnected;

    /// <summary>Fired when the connection is lost (on Unity main thread).</summary>
    public event System.Action OnDisconnected;

    /// <summary>
    /// Override the host/port from config. Must be called before Start().
    /// </summary>
    public void SetHost(string host, int port)
    {
        serverHost = host;
        serverPort = port;
    }

    private TcpClient tcpClient;
    private NetworkStream stream;
    private bool connected;
    private Thread receiveThread;
    private readonly object sendLock = new object();

    // ─── Ping Measurement ──────────────────────────────────────────────
    /// <summary>Latest measured round-trip time in milliseconds.</summary>
    public float LastPingMs { get; private set; }

    /// <summary>Timestamp (ms) when the last heartbeat was sent.</summary>
    private long pingSendTimestamp;

    // ─── Reconnect State ────────────────────────────────────────────────
    private const int MaxReconnectAttempts = 3;
    private int reconnectAttempt;
    private bool reconnecting;
    private Coroutine reconnectCoroutine;

    // Opcodes — must match server/protocol/opcodes.go
    private const byte OpcodeC2SHeartbeat = 21;
    private const byte OpcodeS2CHeartbeat = 0x17;

    private void Start()
    {
        StartCoroutine(ConnectAsync());
    }

    /// <summary>
    /// Connect to server asynchronously via thread pool — does NOT block the Unity main thread.
    /// </summary>
    private Task connectTask;
    private IEnumerator ConnectAsync()
    {
        // Save local reference so scene-change cleanup can observe/dispose it.
        connectTask = Task.Run(() =>
        {
            tcpClient = new TcpClient();
            tcpClient.Connect(serverHost, serverPort);
        });

        // Yield every frame until the connect task completes.
        while (!connectTask.IsCompleted)
            yield return null;

        if (connectTask.IsFaulted)
        {
            Debug.LogError($"[NET] Connect failed: {connectTask.Exception?.InnerException?.Message}");
            // Attempt reconnect
            TryReconnect();
            yield break;
        }

        stream = tcpClient.GetStream();
        connected = true;
        reconnectAttempt = 0;
        reconnecting = false;

        receiveThread = new Thread(ReceiveLoop) { IsBackground = true };
        receiveThread.Start();

        Debug.Log($"[NET] Connected to {serverHost}:{serverPort}");

        // Notify listeners on main thread
        UnityMainThreadDispatcher.Enqueue(() => OnConnected?.Invoke());

        // StartHeartbeat() should be called manually after login success.
    }

    /// <summary>
    /// Begin reconnect with exponential backoff (1s → 2s → 4s).
    /// Called when initial connection fails or connection is lost.
    /// </summary>
    private void TryReconnect()
    {
        if (reconnecting) return;
        if (reconnectAttempt >= MaxReconnectAttempts)
        {
            Debug.LogError($"[NET] Max reconnect attempts ({MaxReconnectAttempts}) reached. Giving up.");
            UnityMainThreadDispatcher.Enqueue(() => OnDisconnected?.Invoke());
            return;
        }

        reconnecting = true;
        reconnectCoroutine = StartCoroutine(ReconnectRoutine());
    }

    private IEnumerator ReconnectRoutine()
    {
        reconnectAttempt++;
        float delay = Mathf.Pow(2, reconnectAttempt - 1); // 1, 2, 4
        Debug.Log($"[NET] Reconnect attempt {reconnectAttempt}/{MaxReconnectAttempts} in {delay}s...");
        yield return new WaitForSeconds(delay);

        // Clean up any stale state
        CleanupConnection();

        connectTask = Task.Run(() =>
        {
            try
            {
                tcpClient = new TcpClient();
                tcpClient.Connect(serverHost, serverPort);
            }
            catch { }
        });

        while (!connectTask.IsCompleted)
            yield return null;

        if (connectTask.IsFaulted || tcpClient == null || !tcpClient.Connected)
        {
            Debug.LogWarning($"[NET] Reconnect attempt {reconnectAttempt} failed.");
            reconnecting = false;
            TryReconnect(); // Try next attempt
            yield break;
        }

        stream = tcpClient.GetStream();
        connected = true;
        reconnecting = false;
        reconnectAttempt = 0;

        receiveThread = new Thread(ReceiveLoop) { IsBackground = true };
        receiveThread.Start();

        Debug.Log($"[NET] Reconnected to {serverHost}:{serverPort}");

        UnityMainThreadDispatcher.Enqueue(() => OnConnected?.Invoke());
    }

    /// <summary>Clean up TCP resources before reconnect.</summary>
    private void CleanupConnection()
    {
        connected = false;
        if (stream != null) { stream.Close(); stream = null; }
        if (tcpClient != null) { tcpClient.Close(); tcpClient = null; }
    }

    private void Update()
    {
        UnityMainThreadDispatcher.Flush();
    }

    public void StartHeartbeat()
    {
        StartCoroutine(HeartbeatLoop());
    }

    private IEnumerator HeartbeatLoop()
    {
        while (connected)
        {
            yield return new WaitForSeconds(30f);
            // Record send timestamp for ping calculation
            pingSendTimestamp = StopwatchGetTimestampMs();
            SendPacket(OpcodeC2SHeartbeat, new byte[0]);
        }
    }

    /// <summary>
    /// High-resolution timestamp for ping measurement.
    /// Uses Stopwatch.GetTimestamp() for sub-millisecond precision.
    /// </summary>
    [MethodImpl(MethodImplOptions.AggressiveInlining)]
    private static long StopwatchGetTimestampMs()
    {
        return System.Diagnostics.Stopwatch.GetTimestamp() /
               (System.Diagnostics.Stopwatch.Frequency / 1000L);
    }

    public void SendPacket(byte opcode, byte[] payload)
    {
        if (stream == null || !connected) return;

        // Packet format: [length uint16][opcode byte][payload bytes]
        ushort length = (ushort)(1 + payload.Length);
        byte[] header = { (byte)(length >> 8), (byte)(length & 0xFF) };

        lock (sendLock)
        {
            try
            {
                stream.Write(header, 0, 2);
                stream.WriteByte(opcode);
                if (payload.Length > 0)
                    stream.Write(payload, 0, payload.Length);
            }
            catch (Exception ex)
            {
                Debug.LogError($"[NET] Send error: {ex.Message}");
                Disconnect();
            }
        }
    }

    private void ReceiveLoop()
    {
        byte[] headerBuf = new byte[2];
        while (connected)
        {
            try
            {
                // Read 2-byte length header
                int read = 0;
                while (read < 2 && connected)
                {
                    int n = stream.Read(headerBuf, read, 2 - read);
                    if (n == 0) { Disconnect(); return; }
                    read += n;
                }

                ushort length = (ushort)((headerBuf[0] << 8) | headerBuf[1]);
                if (length == 0) continue;
                if (length > 4096) { Disconnect(); return; }

                // Read payload
                byte[] payload = new byte[length];
                read = 0;
                while (read < length && connected)
                {
                    int n = stream.Read(payload, read, length - read);
                    if (n == 0) { Disconnect(); return; }
                    read += n;
                }

                byte opcode = payload[0];

                // Handle heartbeat pong — measure ping
                if (opcode == OpcodeS2CHeartbeat)
                {
                    if (pingSendTimestamp != 0)
                    {
                        long now = StopwatchGetTimestampMs();
                        long rtt = now - pingSendTimestamp;
                        if (rtt >= 0)
                            LastPingMs = rtt;
                        pingSendTimestamp = 0;
                    }
                    continue;
                }

                // Process other opcodes on main thread
                byte[] data = new byte[length - 1];
                if (data.Length > 0)
                    Buffer.BlockCopy(payload, 1, data, 0, data.Length);

                UnityMainThreadDispatcher.Enqueue(() =>
                {
                    HandlePacket(opcode, data);
                });
            }
            catch (Exception ex)
            {
                if (connected)
                    Debug.LogError($"[NET] Receive error: {ex.Message}");
                Disconnect();
            }
        }
    }

    private void HandlePacket(byte opcode, byte[] data)
    {
        // Route received packets to game logic via PacketRouter
        var router = GetComponent<PacketRouter>();
        if (router != null)
            router.Route(opcode, data);
    }

    public void Disconnect()
    {
        if (!connected && !reconnecting) return;
        connected = false;
        if (stream != null) { stream.Close(); stream = null; }
        if (tcpClient != null) { tcpClient.Close(); tcpClient = null; }
        StopAllCoroutines();
        Debug.Log("[NET] Disconnected from server.");

        // Trigger reconnect on unexpected disconnects (not during scene teardown)
        if (!isQuitting)
        {
            UnityMainThreadDispatcher.Enqueue(() => OnDisconnected?.Invoke());
            TryReconnect();
        }
    }

    private bool isQuitting;

    private void OnApplicationQuit()
    {
        isQuitting = true;
        Disconnect();
    }

    private void OnDestroy()
    {
        Disconnect();
        // receiveThread is IsBackground → auto-terminates when process exits.
        // stream.Close() in Disconnect() causes ReceiveLoop stream.Read to throw →
        // loop exits naturally. No need for Thread.Abort().
    }
}