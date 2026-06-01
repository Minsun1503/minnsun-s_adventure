package game

import (
	"fmt"
	"io"
	"net"
	"server/ecs"
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
					fmt.Printf("Mock read error: %v\n", err)
				}
				return
			}
			msgChan <- string(buf[:n])
		}
	}()
	return w, msgChan
}

func TestMovementSystem(t *testing.T) {
	registry := ecs.GlobalRegistry
	playerID := registry.NewEntity()

	// Initialize components
	conn, msgChan := createMockConn()
	defer conn.Close()

	registry.SetConnection(playerID, ecs.ConnectionComponent{Conn: conn})
	registry.SetMetadata(playerID, ecs.MetadataComponent{Name: "Hero", Type: "player"})
	registry.SetPosition(playerID, ecs.PositionComponent{MapID: 1, X: 5, Z: 5})

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

	// Read broadcast msg from connection
	select {
	case msg := <-msgChan:
		if !strings.Contains(msg, "Hero moved to position: X=6, Z=6") {
			t.Errorf("Unexpected broadcast message: %q", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for movement broadcast")
	}

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

	// 3. Test blocked tile collision
	success = MovementSystem(playerID, 50, 50) // Blocked square
	if success {
		t.Error("Expected movement to blocked square (50, 50) to be rejected")
	}
	select {
	case msg := <-msgChan:
		if !strings.Contains(msg, "Collision Alert: Path is blocked") {
			t.Errorf("Unexpected collision message: %q", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for collision rejection notice")
	}
}
