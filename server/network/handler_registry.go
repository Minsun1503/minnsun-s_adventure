package network

import (
	"net"

	"server/ecs"
	"server/protocol"
)

// PacketHandler is the function signature for a single binary opcode handler.
// Each handler receives the player's TCP connection, their ECS entity ID,
// and the raw payload bytes (everything after the opcode byte).
type PacketHandler func(conn net.Conn, playerEntity ecs.Entity, payload []byte)

// packetHandlers is the opcode → handler lookup table.
// Replaces the giant switch block in HandleBinaryPacket with an O(1) map lookup.
// Handlers are registered during init() for compile-time safety.
var packetHandlers = make(map[byte]PacketHandler)

// init registers all known opcode handlers.
// Any opcode without a registered handler will fall through to the default
// "unknown opcode" path in HandleBinaryPacket.
func init() {
	// Movement
	RegisterHandler(protocol.OpcodeC2SMove, handleMove)
	RegisterHandler(protocol.OpcodeC2SInv, handleInventory)
	RegisterHandler(protocol.OpcodeC2SUse, handleUseItem)
	RegisterHandler(protocol.OpcodeC2SWarp, handleWarp)
	RegisterHandler(protocol.OpcodeC2SAttack, handleAttack)
	RegisterHandler(protocol.OpcodeC2SInfo, handleInfo)
	RegisterHandler(protocol.OpcodeC2SQuit, handleQuit)
	RegisterHandler(protocol.OpcodeC2SPickup, handlePickup)
	RegisterHandler(protocol.OpcodeC2SEquip, handleEquip)

	// Social
	RegisterHandler(protocol.OpcodeC2SPartyCreate, handlePartyCreate)
	RegisterHandler(protocol.OpcodeC2SPartyInvite, handlePartyInvite)
	RegisterHandler(protocol.OpcodeC2SPartyJoin, handlePartyJoin)

	// Trade
	RegisterHandler(protocol.OpcodeC2STradeInit, handleTradeInit)
	RegisterHandler(protocol.OpcodeC2STradeOffer, handleTradeOffer)
	RegisterHandler(protocol.OpcodeC2STradeConfirm, handleTradeConfirm)
	RegisterHandler(protocol.OpcodeC2STradeCancel, handleTradeCancel)

	// Skills & Chat
	RegisterHandler(protocol.OpcodeC2SSkillCast, handleSkillCast)
	RegisterHandler(protocol.OpcodeC2SChat, handleChat)
	RegisterHandler(protocol.OpcodeC2SHeartbeat, handleHeartbeat)
}

// RegisterHandler adds a packet handler function to the registry.
// Exposed so other packages (e.g., game extensions) can register custom handlers.
func RegisterHandler(opcode byte, handler PacketHandler) {
	packetHandlers[opcode] = handler
}

// DispatchPacket looks up the handler for the given opcode and calls it.
// Returns true if a handler was found and executed; false if the opcode is unknown.
func DispatchPacket(conn net.Conn, playerEntity ecs.Entity, opcode byte, payload []byte) bool {
	if handler, ok := packetHandlers[opcode]; ok {
		handler(conn, playerEntity, payload)
		return true
	}
	return false
}
