package protocol

// Client → Server opcodes
const (
	OpcodeC2SMove         byte = 1
	OpcodeC2SInv          byte = 2
	OpcodeC2SUse          byte = 3
	OpcodeC2SWarp         byte = 4
	OpcodeC2SAttack       byte = 5
	OpcodeC2SInfo         byte = 6
	OpcodeC2SQuit         byte = 7
	OpcodeC2SPickup       byte = 8
	OpcodeC2SEquip        byte = 9
	OpcodeC2SLogin        byte = 10
	OpcodeC2SRegister     byte = 11
	OpcodeC2SPartyCreate  byte = 12
	OpcodeC2SPartyInvite  byte = 13
	OpcodeC2SPartyJoin    byte = 14
	OpcodeC2STradeInit    byte = 15
	OpcodeC2STradeOffer   byte = 16
	OpcodeC2STradeConfirm byte = 17
	OpcodeC2STradeCancel  byte = 18
	OpcodeC2SSkillCast    byte = 19
	OpcodeC2SChat         byte = 20
	OpcodeC2SHeartbeat    byte = 21
)

// Server → Client opcodes — MUST match peakgo/broadcast opcodes (wire format)
const (
	OpcodeS2CSuccess       byte = 0x01
	OpcodeS2CError         byte = 0xFF
	OpcodeS2CSpawnEntity   byte = 0x10
	OpcodeS2CDespawnEntity byte = 0x11
	OpcodeS2CPositionSync  byte = 0x12
	OpcodeS2CStatsSync     byte = 0x13
	OpcodeS2CCombatHit     byte = 0x14
	OpcodeS2CChat          byte = 0x15
	OpcodeS2CNotice        byte = 0x16
	OpcodeS2CHeartbeat     byte = 0x17
)

// Error codes
const (
	ErrCodeServerFull    uint16 = 1
	ErrCodeDatabaseError uint16 = 2
	ErrCodeInternalError uint16 = 3
)
