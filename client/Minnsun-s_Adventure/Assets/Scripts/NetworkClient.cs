using System;
using System.Collections;
using System.Collections.Concurrent;
using System.Net.Sockets;
using System.Threading;
using System.Threading.Tasks;
using UnityEngine;

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
/// TCP network client with binary packet framing, heartbeat, and main-thread dispatch.
/// Attach to a persistent GameObject; set host/port in Inspector; call StartHeartbeat() after login success.
/// </summary>
public class NetworkClient : MonoBehaviour
{
    [SerializeField] private string serverHost = "127.0.0.1";
    [SerializeField] private int serverPort = 1503;

    private TcpClient tcpClient;
    private NetworkStream stream;
    private bool connected;
    private Thread receiveThread;
    private readonly object sendLock = new object();

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
            yield break;
        }

        stream = tcpClient.GetStream();
        connected = true;

        receiveThread = new Thread(ReceiveLoop) { IsBackground = true };
        receiveThread.Start();

        Debug.Log($"[NET] Connected to {serverHost}:{serverPort}");

        // StartHeartbeat() should be called manually after login success.
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
            SendPacket(OpcodeC2SHeartbeat, new byte[0]);
        }
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

                // Handle heartbeat pong silently
                if (opcode == OpcodeS2CHeartbeat)
                    continue;

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
        // Route received packets to game logic
        // Implement based on your game's packet handling
        Debug.Log($"[NET] Received opcode: {opcode}, data length: {data.Length}");
    }

    public void Disconnect()
    {
        connected = false;
        if (stream != null) { stream.Close(); stream = null; }
        if (tcpClient != null) { tcpClient.Close(); tcpClient = null; }
        StopAllCoroutines();
        Debug.Log("[NET] Disconnected from server.");
    }

    private void OnDestroy()
    {
        Disconnect();
        // receiveThread is IsBackground → auto-terminates when process exits.
        // stream.Close() in Disconnect() causes ReceiveLoop stream.Read to throw →
        // loop exits naturally. No need for Thread.Abort().
    }
}