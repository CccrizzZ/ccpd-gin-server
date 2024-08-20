package invoices

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
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

// gorilla websocket
func WsHandler(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Println("upgrade:", err)
		return
	}
	defer ws.Close()

	for {
		// read bytes from client
		_, msg, err := ws.ReadMessage()
		if err != nil {
			fmt.Println("Read Msg Error:", err)
			break
		}
		fmt.Printf("Got Msg: %s\n", msg)

		// unmarshal into msg type
		var inMsg Message
		jsonErr := json.Unmarshal(msg, &inMsg)
		if jsonErr != nil {
			fmt.Println("Cannot Unmarshal JSON")
		}

		// switch on message type
		switch inMsg.Type {
		case InitSignature:
			OnInitSignature(ws, inMsg.Data)
		case SubmitSignature:
			OnSubmitSignature(ws, inMsg.Data)
		}
	}
}

// data should contain invoice number, date, buyer name
func OnInitSignature(ws *websocket.Conn, data interface{}) {
	fmt.Println(data)
	fmt.Println("INIT SIG")
}

// submit the signature record
func OnSubmitSignature(ws *websocket.Conn, data interface{}) {
	fmt.Println(data)
	fmt.Println("SUBMIT SIG")
}
