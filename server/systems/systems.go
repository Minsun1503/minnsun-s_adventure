package systems

import (
	"fmt"
	"server/ecs"
	"server/game"
	"server/peakgo/broadcast"
)

// BroadcastSystem sends a raw byte payload to every entity with an active connection.
func BroadcastSystem(data []byte) {
	frame := broadcast.BuildNotice(broadcast.NoticePayload{Message: string(data)})
	ecs.DefaultRegistry.RangeConnections(func(_ ecs.Entity, conn ecs.ConnectionComponent) bool {
		if conn.Writer != nil {
			conn.Writer.Send(frame)
		}
		return true
	})
}

// BroadcastExcept sends a payload to all connected entities except the excluded one.
func BroadcastExcept(exclude ecs.Entity, data []byte) {
	ecs.DefaultRegistry.RangeConnections(func(id ecs.Entity, conn ecs.ConnectionComponent) bool {
		if id != exclude && conn.Writer != nil {
			conn.Writer.Send(data)
		}
		return true
	})
}

// SendNoticeSystem sends a direct payload to a single entity's connection.
func SendNoticeSystem(entity ecs.Entity, data []byte) {
	game.SendNoticeSystem(entity, data)
}

// GetInfoSystem retrieves formatted combat stats for a target entity.
func GetInfoSystem(target ecs.Entity) (string, error) {
	registry := ecs.DefaultRegistry

	meta, metaOk := registry.GetMetadata(target)
	stats, statsOk := registry.GetStats(target)

	if !metaOk || !statsOk {
		return "", fmt.Errorf("entity %d: required components not found", target)
	}

	return fmt.Sprintf("%s → HP: %d  ATK: %d\r\n", meta.Name, stats.HP, stats.Dam), nil
}
