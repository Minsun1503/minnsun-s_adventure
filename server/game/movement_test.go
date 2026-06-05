package game

import (
	"io"
	"net"
	"server/ecs"
	"server/peakgo/loggate"
	"server/world"
	"strings"
	"testing"
	"time"
)

func init() {
	world.InitializeCollisionMaps()
}

// createMockConn creates a piped connection pair.
// Returns the connection to be attached to the entity, and a channel that yields all data written to it.
func createMockConn() (net.Conn, chan string) {
	r, w := net.Pipe()
	msgChan := make(chan string, 10)
	go func() {
		defer r.Close()
		buf := make([]byte, 1024)
		for {
			_ = r.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := r.Read(buf)
			if err != nil {
				close(msgChan)
				if err != io.EOF && !strings.Contains(err.Error(), "timeout") {
					loggate.Errorf("Mock read error: %v", err)
				}
				return
			}
			msgChan <- string(buf[:n])
		}
	}()
	return w, msgChan
}

func TestMovementSystem(t *testing.T) {
	registry := ecs.DefaultRegistry
	playerID := registry.NewEntity()

	// Initialize components
	conn, msgChan := createMockConn()
	defer conn.Close()

	registry.SetConnection(playerID, ecs.ConnectionComponent{Conn: conn})
	registry.SetMetadata(playerID, ecs.MetadataComponent{Name: "Hero", Type: ecs.EntityPlayer})
	registry.SetPosition(playerID, ecs.PositionComponent{MapID: 1, X: 5, Z: 5})

	// Fix AOI: Explicitly register entity in SpatialGrid before AOI queries
	world.GlobalSpatialGrid.UpdateEntityPosition(playerID, ecs.PositionComponent{MapID: 1, X: 5, Z: 5})

	// 1. Test valid movement
	success := MovementSystem(playerID, 6, 6)
	if !success {
		t.Error("Expected valid movement (6, 6) to succeed")
	}

	// Verify position in registry
	pos, ok := registry.GetPosition(playerID)
	if !ok || pos.X != 6 || pos.Z != 6 {
		t.Errorf("Expected player position to be (6, 6), got %+v", pos)
	}

	// Verify position in spatial grid
	chk, ok := world.GlobalSpatialGrid.GetEntityChunk(playerID)
	if !ok || chk.X != 0 || chk.Z != 0 {
		t.Errorf("Expected player in spatial grid chunk (0,0), got ok=%t chk=%+v", ok, chk)
	}

	// No broadcast assertion here because BroadcastToNeighbors excludes the source entity.
	// With only one player in the test, no binary frame reaches the connection.

	// 2. Test out-of-bounds movement
	success = MovementSystem(playerID, -1, 5)
	if success {
		t.Error("Expected out-of-bounds movement to -1 to be rejected")
	}
	select {
	case msg := <-msgChan:
		if !strings.Contains(msg, "Movement rejected! Out of bounds") {
			t.Errorf("Unexpected rejection message: %q", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for boundary rejection notice")
	}

	// 3. Test blocked tile collision — use a neighboring blocked tile within MaxMoveDistance=2
	// (7, 6) is within Chebyshev distance 1 from current pos (6, 6), but tile is blocked.
	success = MovementSystem(playerID, 7, 6) // Blocked neighbor
	if success {
		t.Error("Expected movement to blocked tile (7, 6) to be rejected")
	}
	select {
	case msg := <-msgChan:
		if !strings.Contains(msg, "Collision Alert: Path is blocked") {
			t.Errorf("Unexpected collision message: %q", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for collision rejection notice")
	}

	// 4. Test anti-teleport / speed-hack detection
	// Jump from last valid pos (6,6) to (20,20) — dx=14, dz=14 both exceed MaxMoveDistance=2.
	success = MovementSystem(playerID, 20, 20)
	if success {
		t.Error("Expected teleport jump to (20, 20) to be rejected by anti-cheat")
	}
	select {
	case msg := <-msgChan:
		if !strings.Contains(msg, "Movement rejected! Teleport/Speed hack detected.") {
			t.Errorf("Unexpected anti-cheat message: %q", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for anti-cheat rejection notice")
	}
}
