package network

import "net"

// SendNoticeToPlayer is our low-resource helper tool. 
// It writes a message directly to a specific player's socket connection.
func SendNoticeToPlayer(message string, conn net.Conn) {
	if conn != nil {
		conn.Write([]byte(message))
	}
}
