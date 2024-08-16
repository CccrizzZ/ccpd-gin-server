package invoices

import (
	"fmt"

	socketio "github.com/googollee/go-socket.io"
)

// on client connection
func OnConnect(s socketio.Conn) error {
	s.SetContext("")
	fmt.Println("Connected: ")
	fmt.Println(s.ID())
	return nil
}

// on init signature from client
func InitSignature(s socketio.Conn, msg string) {
	fmt.Println("Notice:", msg)
	s.Emit("reply", "have "+msg)
}

// on client disconnected from socket
func OnDisconnect(s socketio.Conn, reason string) {
	fmt.Println("Connection Closed:", reason)
}

// on socket error
func OnError(s socketio.Conn, e error) {
	fmt.Println("Socket Error:", e)
}
