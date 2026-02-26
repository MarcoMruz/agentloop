package server

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/google/uuid"
)

type Client struct {
	id            string
	conn          net.Conn
	mu            sync.Mutex
	subscriptions map[string]bool // sessionId → subscribed
}

func NewClient(conn net.Conn) *Client {
	return &Client{
		id:            uuid.New().String()[:8],
		conn:          conn,
		subscriptions: make(map[string]bool),
	}
}

func (c *Client) ID() string { return c.id }

func (c *Client) Subscribe(sessionId string)   { c.subscriptions[sessionId] = true }
func (c *Client) Unsubscribe(sessionId string)  { delete(c.subscriptions, sessionId) }
func (c *Client) IsSubscribed(sessionId string) bool { return c.subscriptions[sessionId] }

func (c *Client) SendResult(id int, result any) {
	c.send(JSONRPCResponse{JSONRPC: "2.0", ID: &id, Result: result})
}

func (c *Client) SendError(id *int, code int, msg string) {
	c.send(JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}})
}

func (c *Client) SendNotification(method string, params any) {
	c.send(JSONRPCNotification{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) send(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil { return }
	fmt.Fprintf(c.conn, "%s\n", data)
}
