using System;
using System.Collections;
using System.Collections.Concurrent;
using System.Text;
using UnityEngine;
using NativeWebSocket;

/// <summary>
/// WebSocket network client for WebGL builds using NativeWebSocket.
/// Matches the binary framing protocol of NetworkClient.cs:
///   [length uint16 BE][opcode uint8][payload N-bytes]
///
/// Requires NativeWebSocket package:
///   https://github.com/endel/NativeWebSocket.git#upm
/// </summary>
public class NetworkClientWS : MonoBehaviour
{
    [SerializeField] private string serverUrl = "ws://127.0.0.1:8081/ws";

    private WebSocket ws;
    private bool connected;
    private readonly ConcurrentQueue<Action> dispatchQueue = new ConcurrentQueue<Action>();

    // Opcodes — must match server/protocol/opcodes.go
    private const byte OpcodeC2SHeartbeat = 21;
    private const byte OpcodeS2CHeartbeat = 0x17;

    private void Start()
    {
        StartCoroutine(ConnectAsync());
    }

    private IEnumerator ConnectAsync()
    {
        ws = new WebSocket(serverUrl);

        ws.OnOpen += () =>
        {
            connected = true;
            Debug.Log($"[WS] Connected to {serverUrl}");
        };

        ws.OnError += (errorMsg) =>
        {
            Debug.LogError($"[WS] Error: {errorMsg}");
        };

        ws.OnClose += (closeCode) =>
        {
            connected = false;
            Debug.Log($"[WS] Closed (code: {closeCode})");
        };

        ws.OnMessage += (bytes) =>
        {
            // bytes is the raw binary message — each message is one framed packet
            HandleRawMessage(bytes);
        };

        var connectTask = ws.Connect();
        while (!connectTask.IsCompleted)
            yield return null;

        if (connectTask.IsFaulted)
        {
            Debug.LogError($"[WS] Connect failed: {connectTask.Exception?.InnerException?.Message}");
            yield break;
        }

        Debug.Log($"[WS] Connected to {serverUrl}");

        // StartHeartbeat() should be called manually after login success.
    }

    private void Update()
    {
        // Pump WebSocket event loop (required on Unity main thread when not using WebGL threads)
#if !UNITY_WEBGL || UNITY_EDITOR
        if (ws != null)
        {
            // DispatchMessageQueue is the thread-safe pump for NativeWebSocket
            ws.DispatchMessageQueue();
        }
#endif
        // Flush queued packet handlers on main thread
        while (dispatchQueue.TryDequeue(out Action action))
        {
            try { action(); }
            catch (Exception ex) { Debug.LogError($"[WS] Dispatch error: {ex.Message}"); }
        }
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

    /// <summary>
    /// Build a binary frame and send it as a WebSocket binary message.
    /// Packet format: [length uint16 BE][opcode byte][payload bytes]
    /// </summary>
    public void SendPacket(byte opcode, byte[] payload)
    {
        if (ws == null || !connected) return;

        ushort length = (ushort)(1 + payload.Length);
        byte[] frame = new byte[2 + length];

        // Length prefix (Big Endian)
        frame[0] = (byte)(length >> 8);
        frame[1] = (byte)(length & 0xFF);

        // Opcode
        frame[2] = opcode;

        // Payload
        if (payload.Length > 0)
            Buffer.BlockCopy(payload, 0, frame, 3, payload.Length);

        _ = ws.Send(frame).ContinueWith(t =>
        {
            if (t.IsFaulted)
            {
                Debug.LogError($"[WS] Send failed: {t.Exception?.InnerException?.Message}");
                // Marshal Disconnect to main thread — StopAllCoroutines() is Unity API.
                dispatchQueue.Enqueue(() => Disconnect());
            }
        });
    }

    /// <summary>
    /// Handle a raw binary WebSocket message.
    /// Each message is one complete framed packet: [length][opcode][payload].
    /// Parsed on receive thread, dispatched to main thread via queue.
    /// </summary>
    private void HandleRawMessage(byte[] bytes)
    {
        if (bytes.Length < 3) return; // minimum: 2 header + 1 opcode

        // Read Big Endian length
        ushort declaredLen = (ushort)((bytes[0] << 8) | bytes[1]);

        // Validate declared length matches actual payload
        int payloadStart = 2;
        int available = bytes.Length - payloadStart;
        if (declaredLen > available)
        {
            Debug.LogWarning($"[WS] Declared length {declaredLen} exceeds message size {available}, truncating");
            declaredLen = (ushort)available;
        }
        if (declaredLen == 0) return;

        byte opcode = bytes[payloadStart];
        int dataLen = declaredLen - 1;

        // Heartbeat pong — handle silently, no dispatch
        if (opcode == OpcodeS2CHeartbeat)
            return;

        byte[] data = null;
        if (dataLen > 0)
        {
            data = new byte[dataLen];
            Buffer.BlockCopy(bytes, payloadStart + 1, data, 0, dataLen);
        }
        else
        {
            data = new byte[0];
        }

        // Capture locals for closure
        byte capturedOpcode = opcode;
        byte[] capturedData = data;

        dispatchQueue.Enqueue(() =>
        {
            HandlePacket(capturedOpcode, capturedData);
        });
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
        connected = false;
        if (ws != null)
        {
            var closeTask = ws.Close();
            ws = null;
        }
        StopAllCoroutines();
        Debug.Log("[WS] Disconnected from server.");
    }

    private void OnDestroy()
    {
        Disconnect();
    }
}