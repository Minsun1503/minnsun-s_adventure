using System;
using System.Text;
using UnityEngine;

/// <summary>
/// Binary packet decoders — parses raw payload bytes into typed structs.
/// All multi-byte integers are Big Endian, matching the server's peakgo/broadcast wire format.
/// Each decode method returns null on parse failure (logs warning, never throws).
/// </summary>
public static class Decoders
{
    // ─── Helper ─────────────────────────────────────────────────────────

    private static ushort ReadUint16BE(byte[] data, int offset)
    {
        return (ushort)((data[offset] << 8) | data[offset + 1]);
    }

    private static uint ReadUint32BE(byte[] data, int offset)
    {
        return (uint)((data[offset] << 24) | (data[offset + 1] << 16) | (data[offset + 2] << 8) | data[offset + 3]);
    }

    private static ulong ReadUint64BE(byte[] data, int offset)
    {
        return ((ulong)data[offset] << 56) | ((ulong)data[offset + 1] << 48) | ((ulong)data[offset + 2] << 40) | ((ulong)data[offset + 3] << 32)
             | ((ulong)data[offset + 4] << 24) | ((ulong)data[offset + 5] << 16) | ((ulong)data[offset + 6] << 8) | data[offset + 7];
    }

    // ─── Struct Definitions ─────────────────────────────────────────────

    public struct SpawnPacket
    {
        public ulong EntityID;
        public byte  Type;       // 0=player, 1=monster, 2=ground_item
        public int   MapID;
        public int   X;
        public int   Z;
        public string Name;
    }

    public struct DespawnPacket
    {
        public ulong EntityID;
    }

    public struct PositionPacket
    {
        public ulong EntityID;
        public int   X;
        public int   Z;
    }

    public struct StatsPacket
    {
        public ulong EntityID;
        public int   HP;
        public int   MaxHP;
        public int   MP;
        public int   MaxMP;
        public int   Dam;
        public int   Level;
    }

    public struct CombatHitPacket
    {
        public ulong AttackerID;
        public ulong TargetID;
        public int   Damage;
        public int   TargetHP;
        public byte  Killed;
    }

    public struct ChatPacket
    {
        public byte   Channel;
        public string SenderName;
        public string Message;
    }

    public struct NoticePacket
    {
        public string Message;
    }

    public struct SuccessPacket
    {
        public ulong EntityID;
        public string Message;
    }

    // ─── Decoders ───────────────────────────────────────────────────────

    /// <summary>
    /// Decode SpawnEntity payload (opcode 0x10).
    /// Layout: EntityID(8) + Type(1) + MapID(4) + X(4) + Z(4) + NameLen(1) + Name(N)
    /// Total fixed = 22 bytes before variable name.
    /// </summary>
    public static SpawnPacket? DecodeSpawn(byte[] data)
    {
        if (data == null || data.Length < 22)
        {
            Debug.LogWarning("[Decode] Spawn payload too short");
            return null;
        }

        SpawnPacket p;
        p.EntityID = ReadUint64BE(data, 0);
        p.Type     = data[8];
        p.MapID    = (int)ReadUint32BE(data, 9);
        p.X        = (int)ReadUint32BE(data, 13);
        p.Z        = (int)ReadUint32BE(data, 17);
        byte nameLen = data[21];
        int expected = 22 + nameLen;
        if (data.Length < expected)
        {
            Debug.LogWarning($"[Decode] Spawn payload truncated: need {expected}, got {data.Length}");
            return null;
        }
        p.Name = Encoding.UTF8.GetString(data, 22, nameLen);
        return p;
    }

    /// <summary>
    /// Decode DespawnEntity payload (opcode 0x11).
    /// Layout: EntityID(8)
    /// </summary>
    public static DespawnPacket? DecodeDespawn(byte[] data)
    {
        if (data == null || data.Length < 8)
        {
            Debug.LogWarning("[Decode] Despawn payload too short");
            return null;
        }

        DespawnPacket p;
        p.EntityID = ReadUint64BE(data, 0);
        return p;
    }

    /// <summary>
    /// Decode PositionSync payload (opcode 0x12).
    /// Layout: EntityID(8) + X(4) + Z(4) = 16 bytes
    /// </summary>
    public static PositionPacket? DecodePosition(byte[] data)
    {
        if (data == null || data.Length < 16)
        {
            Debug.LogWarning("[Decode] Position payload too short");
            return null;
        }

        PositionPacket p;
        p.EntityID = ReadUint64BE(data, 0);
        p.X        = (int)ReadUint32BE(data, 8);
        p.Z        = (int)ReadUint32BE(data, 12);
        return p;
    }

    /// <summary>
    /// Decode StatsSync payload (opcode 0x13).
    /// Layout: EntityID(8) + HP:MaxHP packed(8) + MP:MaxMP packed(8) + Dam:Level packed(8) = 32 bytes
    /// Each packed uint64: high 32 bits = first value, low 32 bits = second value.
    /// </summary>
    public static StatsPacket? DecodeStats(byte[] data)
    {
        if (data == null || data.Length < 32)
        {
            Debug.LogWarning("[Decode] Stats payload too short");
            return null;
        }

        StatsPacket p;
        p.EntityID = ReadUint64BE(data, 0);

        ulong hpMp   = ReadUint64BE(data, 8);
        ulong mpMp   = ReadUint64BE(data, 16);
        ulong damLvl = ReadUint64BE(data, 24);

        p.HP    = (int)(hpMp >> 32);
        p.MaxHP = (int)(hpMp & 0xFFFFFFFF);
        p.MP    = (int)(mpMp >> 32);
        p.MaxMP = (int)(mpMp & 0xFFFFFFFF);
        p.Dam   = (int)(damLvl >> 32);
        p.Level = (int)(damLvl & 0xFFFFFFFF);

        return p;
    }

    /// <summary>
    /// Decode CombatHit payload (opcode 0x14).
    /// Layout: AttackerID(8) + TargetID(8) + Damage(4) + TargetHP(4) + Killed(1) = 25 bytes
    /// </summary>
    public static CombatHitPacket? DecodeCombat(byte[] data)
    {
        if (data == null || data.Length < 25)
        {
            Debug.LogWarning("[Decode] CombatHit payload too short");
            return null;
        }

        CombatHitPacket p;
        p.AttackerID = ReadUint64BE(data, 0);
        p.TargetID   = ReadUint64BE(data, 8);
        p.Damage     = (int)ReadUint32BE(data, 16);
        p.TargetHP   = (int)ReadUint32BE(data, 20);
        p.Killed     = data[24];
        return p;
    }

    /// <summary>
    /// Decode Chat payload (opcode 0x15).
    /// Layout: Channel(1) + SenderNameLen(1) + SenderName(N) + MessageLen(2) + Message(M)
    /// </summary>
    public static ChatPacket? DecodeChat(byte[] data)
    {
        if (data == null || data.Length < 4)
        {
            Debug.LogWarning("[Decode] Chat payload too short");
            return null;
        }

        ChatPacket p;
        p.Channel    = data[0];
        byte nameLen = data[1];

        if (data.Length < 2 + nameLen + 2)
        {
            Debug.LogWarning("[Decode] Chat payload truncated during name");
            return null;
        }

        p.SenderName = Encoding.UTF8.GetString(data, 2, nameLen);
        int msgOffset = 2 + nameLen;
        ushort msgLen = ReadUint16BE(data, msgOffset);

        if (data.Length < msgOffset + 2 + msgLen)
        {
            Debug.LogWarning("[Decode] Chat payload truncated during message");
            return null;
        }

        p.Message = Encoding.UTF8.GetString(data, msgOffset + 2, msgLen);
        return p;
    }

    /// <summary>
    /// Decode Success payload (opcode 0x01).
    /// Layout: EntityID(8) + MessageLen(2) + Message(N)
    /// Used to set LocalPlayerID from a trusted server source.
    /// </summary>
    public static SuccessPacket? DecodeSuccess(byte[] data)
    {
        if (data == null || data.Length < 10) // 8 + 2 minimum
        {
            Debug.LogWarning("[Decode] Success payload too short");
            return null;
        }

        SuccessPacket p;
        p.EntityID = ReadUint64BE(data, 0);
        ushort msgLen = ReadUint16BE(data, 8);
        if (data.Length < 10 + msgLen)
        {
            Debug.LogWarning("[Decode] Success payload truncated");
            return null;
        }
        p.Message = Encoding.UTF8.GetString(data, 10, msgLen);
        return p;
    }

    /// <summary>
    /// Decode Notice payload (opcode 0x16).
    /// Layout: raw UTF-8 string (remaining bytes after header).
    /// </summary>
    public static NoticePacket? DecodeNotice(byte[] data)
    {
        if (data == null || data.Length == 0)
        {
            Debug.LogWarning("[Decode] Notice payload empty");
            return null;
        }

        NoticePacket p;
        p.Message = Encoding.UTF8.GetString(data, 0, data.Length);
        return p;
    }
}