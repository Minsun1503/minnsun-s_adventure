/// <summary>
/// Server-to-Client opcode constants — must match server/peakgo/broadcast/broadcast.go
/// and server/protocol/opcodes.go exactly.
/// </summary>
public static class Opcodes
{
    // Client → Server (for reference only — client sends via OpcodeC2S*)
    public const byte C2SLogin        = 10;
    public const byte C2SRegister     = 11;
    public const byte C2SMove         = 1;
    public const byte C2SAttack       = 5;
    public const byte C2SChat         = 20;
    public const byte C2SHeartbeat    = 21;

    // Server → Client — these are actually used for packet routing
    public const byte S2CSpawnEntity   = 0x10;
    public const byte S2CDespawnEntity = 0x11;
    public const byte S2CPositionSync  = 0x12;
    public const byte S2CStatsSync     = 0x13;
    public const byte S2CCombatHit     = 0x14;
    public const byte S2CChat          = 0x15;
    public const byte S2CNotice        = 0x16;
    public const byte S2CHeartbeat     = 0x17;
}