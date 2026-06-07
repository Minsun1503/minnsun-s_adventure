using System;
using System.Text;
using UnityEngine;

/// <summary>
/// Zero-allocation-style C2S packet payload builders for the Minnsun's Adventure
/// binary protocol. All multi-byte integers are Big Endian, matching the server's
/// peakgo/codec wire format.
///
/// Each method writes directly into a caller-provided or newly-allocated byte[].
///
/// Every C2S packet is prefixed with a 4-byte random traceId so the server can
/// correlate client logs with server logs (two-way traceability).
/// </summary>
public static class PacketWriter
{
    // ─── Trace ID ────────────────────────────────────────────────────────
    private static readonly System.Random TraceRng = new System.Random();

    /// <summary>
    /// Generate 4 random bytes as a trace identifier for this C2S packet.
    /// </summary>
    private static byte[] GenerateTraceId()
    {
        byte[] traceId = new byte[4];
        lock (TraceRng)
        {
            TraceRng.NextBytes(traceId);
        }
        return traceId;
    }

    // ─── Helpers ─────────────────────────────────────────────────────────

    /// <summary>Write a Big-Endian int32 into dst at offset.</summary>
    private static void WriteInt32BE(byte[] dst, int offset, int value)
    {
        dst[offset]     = (byte)(value >> 24);
        dst[offset + 1] = (byte)(value >> 16);
        dst[offset + 2] = (byte)(value >> 8);
        dst[offset + 3] = (byte)value;
    }

    /// <summary>Write a Big-Endian uint64 into dst at offset.</summary>
    private static void WriteUint64BE(byte[] dst, int offset, ulong value)
    {
        dst[offset]     = (byte)(value >> 56);
        dst[offset + 1] = (byte)(value >> 48);
        dst[offset + 2] = (byte)(value >> 40);
        dst[offset + 3] = (byte)(value >> 32);
        dst[offset + 4] = (byte)(value >> 24);
        dst[offset + 5] = (byte)(value >> 16);
        dst[offset + 6] = (byte)(value >> 8);
        dst[offset + 7] = (byte)value;
    }

    // ─── C2S Payload Builders ───────────────────────────────────────────

    /// <summary>
    /// Build a MOVE packet payload (opcode 1).
    /// Format: [TraceId (4B)] [X int32 BE (4B)] [Z int32 BE (4B)] = 12 bytes
    /// </summary>
    public static byte[] WriteMove(int x, int z)
    {
        byte[] traceId = GenerateTraceId();
        byte[] payload = new byte[4 + 8];
        Buffer.BlockCopy(traceId, 0, payload, 0, 4);
        WriteInt32BE(payload, 4, x);
        WriteInt32BE(payload, 8, z);
        return payload;
    }

    /// <summary>
    /// Build an ATTACK packet payload (opcode 5).
    /// Format: [TraceId (4B)] [TargetID uint64 BE (8B)] = 12 bytes
    /// </summary>
    public static byte[] WriteAttack(ulong targetEntityID)
    {
        byte[] traceId = GenerateTraceId();
        byte[] payload = new byte[4 + 8];
        Buffer.BlockCopy(traceId, 0, payload, 0, 4);
        WriteUint64BE(payload, 4, targetEntityID);
        return payload;
    }

    /// <summary>
    /// Build a LOGIN packet payload (opcode 10).
    /// Format: [TraceId (4B)] [UsernameLen uint8 (1B)] [Username UTF-8 (N)] [PasswordLen uint8 (1B)] [Password UTF-8 (M)]
    /// </summary>
    public static byte[] WriteLogin(string username, string password)
    {
        byte[] traceId = GenerateTraceId();
        byte[] u = Encoding.UTF8.GetBytes(username);
        byte[] p = Encoding.UTF8.GetBytes(password);
        byte[] payload = new byte[4 + 2 + u.Length + p.Length];
        Buffer.BlockCopy(traceId, 0, payload, 0, 4);
        payload[4] = (byte)u.Length;
        Buffer.BlockCopy(u, 0, payload, 5, u.Length);
        payload[5 + u.Length] = (byte)p.Length;
        Buffer.BlockCopy(p, 0, payload, 6 + u.Length, p.Length);
        return payload;
    }

    /// <summary>
    /// Build a REGISTER packet payload (opcode 11).
    /// Format: [TraceId (4B)] [UsernameLen uint8 (1B)] [Username UTF-8 (N)] [PasswordLen uint8 (1B)] [Password UTF-8 (M)]
    /// </summary>
    public static byte[] WriteRegister(string username, string password)
    {
        byte[] traceId = GenerateTraceId();
        byte[] u = Encoding.UTF8.GetBytes(username);
        byte[] p = Encoding.UTF8.GetBytes(password);
        byte[] payload = new byte[4 + 2 + u.Length + p.Length];
        Buffer.BlockCopy(traceId, 0, payload, 0, 4);
        payload[4] = (byte)u.Length;
        Buffer.BlockCopy(u, 0, payload, 5, u.Length);
        payload[5 + u.Length] = (byte)p.Length;
        Buffer.BlockCopy(p, 0, payload, 6 + u.Length, p.Length);
        return payload;
    }

    /// <summary>
    /// Build a CHAT packet payload (opcode 20).
    /// Format: [TraceId (4B)] [Message UTF-8 (N)].
    /// </summary>
    public static byte[] WriteChat(string message)
    {
        byte[] traceId = GenerateTraceId();
        byte[] msg = Encoding.UTF8.GetBytes(message);
        byte[] payload = new byte[4 + msg.Length];
        Buffer.BlockCopy(traceId, 0, payload, 0, 4);
        Buffer.BlockCopy(msg, 0, payload, 4, msg.Length);
        return payload;
    }

    /// <summary>
    /// Build a HEARTBEAT packet payload (opcode 21).
    /// Format: [TraceId (4B)] = 4 bytes.
    /// </summary>
    public static byte[] WriteHeartbeat()
    {
        return GenerateTraceId();
    }

    // ─── Convenience: Build framed packet directly ──────────────────────

    /// <summary>
    /// Build a complete framed packet: [Length uint16 BE][Opcode byte][Payload...]
    /// Returns the full frame ready for SendPacket().
    /// </summary>
    public static byte[] BuildFrame(byte opcode, byte[] payload)
    {
        ushort length = (ushort)(1 + payload.Length);
        byte[] frame = new byte[2 + length];
        frame[0] = (byte)(length >> 8);
        frame[1] = (byte)(length & 0xFF);
        frame[2] = opcode;
        if (payload.Length > 0)
            Buffer.BlockCopy(payload, 0, frame, 3, payload.Length);
        return frame;
    }
}