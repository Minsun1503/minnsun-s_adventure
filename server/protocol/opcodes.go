package protocol

// ──────────────────────────────────────────────────────────────────────────────
// Centralized Opcode Registry
//
// All binary protocol opcodes between Server ↔ Client are defined here.
// This is the single source of truth for packet routing.
//
// Naming convention: Opcode + Direction prefix + Feature name
//   C2S = Client → Server  (inbound)
//   S2C = Server → Client  (outbound, reserved for future response packets)
//
// To add a new opcode:
//   1. Define the constant here with the next available ID.
//   2. Add the case branch in handleBinaryPacket (server.go).
//   3. Update mock_client.go if needed.
// ──────────────────────────────────────────────────────────────────────────────

// ─── Client → Server (C2S) Opcodes ──────────────────────────────────────────

const (
	OpcodeC2SMove     byte = 1  // Payload: [X int32 BE][Z int32 BE]
	OpcodeC2SInv      byte = 2  // Payload: empty (request bag contents)
	OpcodeC2SUse      byte = 3  // Payload: [ItemID uint64 BE]
	OpcodeC2SWarp     byte = 4  // Payload: [MapID int32 BE][X int32 BE][Z int32 BE]
	OpcodeC2SAttack   byte = 5  // Payload: [TargetEntityID uint64 BE]
	OpcodeC2SInfo     byte = 6  // Payload: [TargetEntityID uint64 BE]
	OpcodeC2SQuit     byte = 7  // Payload: empty (graceful disconnect)
	OpcodeC2SPickup   byte = 8  // Payload: [GroundItemEntityID uint64 BE]
	OpcodeC2SEquip    byte = 9  // Payload: [ItemTemplateID uint64 BE]
	OpcodeC2SLogin    byte = 10 // Payload: [UsernameLen uint8][Username UTF-8][PasswordLen uint8][Password UTF-8]
	OpcodeC2SRegister byte = 11 // Payload: same as LOGIN (auto-create account)
)

// ─── Server → Client (S2C) Opcodes ──────────────────────────────────────────

const (
	OpcodeS2CError byte = 0xFF // Server error response — see error_packet.go
	// Future S2C opcodes:
	// OpcodeS2CSpawnEntity   byte = 0x10 // broadcast new entity to client
	// OpcodeS2CDespawnEntity byte = 0x11 // broadcast entity removal
	// OpcodeS2CPositionSync  byte = 0x12 // authoritative position update
	// OpcodeS2CStatsSync     byte = 0x13 // push HP/damage/stats changes
	// OpcodeS2CInventorySync byte = 0x14 // push full inventory snapshot
	// OpcodeS2CChatMessage   byte = 0x15 // structured chat packet
)

// ─── Error Codes (sub-codes within OpcodeS2CError) ──────────────────────────

const (
	ErrCodeServerFull    uint16 = 1 // Login queue saturated
	ErrCodeDatabaseError uint16 = 2 // DB write/read timeout or failure
	ErrCodeInternalError uint16 = 3 // Unexpected server-side panic or state corruption
)
