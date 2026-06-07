using System;
using System.Text;
using UnityEngine;

/// <summary>
/// Zero-allocation-style C2S packet payload builders for the Minnsun's Adventure
/// binary protocol. All multi-byte integers are Big Endian, matching the server's
/// peakgo/codec wire format.
///
/// Each method writes directly into a caller-provided or newly-allocated byte[].
/// </summary>
public static class PacketWriter
{
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
    /// Format: [X int32 BE (4B)] [Z int32 BE (4B)] = 8 bytes
    /// </summary>
    public static byte[] WriteMove(int x, int z)
    {
        byte[] payload = new byte[8];
        WriteInt32BE(payload, 0, x);
        WriteInt32BE(payload, 4, z);
        return payload;
    }

    /// <summary>
    /// Build an ATTACK packet payload (opcode 5).
    /// Format: [TargetID uint64 BE (8B)] = 8 bytes
    /// </summary>
    public static byte[] WriteAttack(ulong targetEntityID)
    {
        byte[] payload = new byte[8];
        WriteUint64BE(payload, 0, targetEntityID);
        return payload;
    }

    /// <summary>
    /// Build a LOGIN packet payload (opcode 10).
    /// Format: [UsernameLen uint8 (1B)] [Username UTF-8 (N)] [PasswordLen uint8 (1B)] [Password UTF-8 (M)]
    /// </summary>
    public static byte[] WriteLogin(string username, string password)
    {
        byte[] u = Encoding.UTF8.GetBytes(username);
        byte[] p = Encoding.UTF8.GetBytes(password);
        byte[] payload = new byte[2 + u.Length + p.Length];
        payload[0] = (byte)u.Length;
        Buffer.BlockCopy(u, 0, payload, 1, u.Length);
        payload[1 + u.Length] = (byte)p.Length;
        Buffer.BlockCopy(p, 0, payload, 2 + u.Length, p.Length);
        return payload;
    }

    /// <summary>
    /// Build a CHAT packet payload (opcode 20).
    /// Format: raw UTF-8 message bytes.
    /// </summary>
    public static byte[] WriteChat(string message)
    {
        return Encoding.UTF8.GetBytes(message);
    }

    /// <summary>
    /// Build a HEARTBEAT packet payload (opcode 21).
    /// Format: empty (0 bytes).
    /// </summary>
    public static byte[] WriteHeartbeat()
    {
        return new byte[0];
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