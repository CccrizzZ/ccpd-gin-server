package invoices

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// types of message emitted by client
const (
	InitSignature   string = "initSignature"
	SubmitSignature string = "submitSignature"
)

// server object
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// connected clients array
var clients = make(map[string]*Client)
var clientsMutex sync.RWMutex

// gorilla websocket
func WsHandler(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Println("upgrade:", err)
		return
	}
	defer ws.Close()

	// create a new client and append it into the array
	newClient := &Client{
		conn: ws,
		send: make(chan []byte),
	}
	clientID := uuid.NewString()
	clientsMutex.Lock()
	clients[clientID] = newClient
	clientsMutex.Unlock()

	fmt.Println(clients)

	// the read message loop
	for {
		// read bytes from client
		_, msg, err := ws.ReadMessage()
		if err != nil {
			fmt.Println("Read Msg Error:", err)
			break
		}
		fmt.Printf("Got Msg: %s\n", msg)

		// unpack json into msg type
		var inMsg Message
		jsonErr := json.Unmarshal(msg, &inMsg)
		if jsonErr != nil {
			fmt.Println("Cannot Unmarshal JSON")
		}

		// switch on message type
		switch inMsg.Type {
		case InitSignature:
			OnInitSignature(newClient, inMsg)
		case SubmitSignature:
			OnSubmitSignature(ws, inMsg)
		}
	}
	clientsMutex.Lock()
	clients[clientID].conn.Close()
	delete(clients, clientID)
	clientsMutex.Unlock()
}

// broadcast to all connected clients
func bcast(msg []byte) {
	for clientId, client := range clients {
		if clientId != "" {
			err := client.conn.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				fmt.Println("error broadcasting:", err)
			}
		}
		// client.conn.Close()
	}
}

// data should contain invoice number, date, buyer name
func OnInitSignature(client *Client, msg Message) {
	// parse json
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		fmt.Println("error marshaling msg: ", err.Error())
		return
	}
	fmt.Println("INIT SIG")
	// broadcast
	bcast(msgBytes)
}

// submit the signature record
func OnSubmitSignature(ws *websocket.Conn, msg Message) {
	fmt.Println("SUBMIT SIG")
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		fmt.Println("error marshaling msg: ", err.Error())
	}
	// broadcast
	bcast(msgBytes)

}
