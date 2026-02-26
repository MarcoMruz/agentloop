package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      *int      `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type JSONRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	socketPath string
	listener   net.Listener
	handler    *Handler
	clients    map[string]*Client
	mu         sync.RWMutex
}

func New(socketPath string, handler *Handler) *Server {
	return &Server{
		socketPath: socketPath,
		handler:    handler,
		clients:    make(map[string]*Client),
	}
}

func (s *Server) Start(ctx context.Context) error {
	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0755); err != nil {
		return err
	}
	// Remove stale socket
	os.Remove(s.socketPath)

	var err error
	s.listener, err = net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.socketPath, err)
	}
	// Permissions: owner only
	os.Chmod(s.socketPath, 0700)

	slog.Info("AgentLoop server listening", "socket", s.socketPath)

	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil { return nil } // shutdown
			slog.Warn("accept error", "error", err)
			continue
		}
		go s.handleConnection(ctx, conn)
	}
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	client := NewClient(conn)
	clientID := client.ID()
	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, clientID)
		s.mu.Unlock()
		conn.Close()
		slog.Debug("client disconnected", "clientId", clientID)
	}()

	slog.Debug("client connected", "clientId", clientID)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" { continue }

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			client.SendError(nil, -32700, "Parse error")
			continue
		}

		// Dispatch to handler
		result, rpcErr := s.handler.Handle(ctx, client, req)
		if req.ID != nil {
			if rpcErr != nil {
				client.SendError(req.ID, rpcErr.Code, rpcErr.Message)
			} else {
				client.SendResult(*req.ID, result)
			}
		}
	}
}

// Broadcast sends a notification to all connected clients subscribed to a session.
func (s *Server) Broadcast(sessionId string, method string, params any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		if client.IsSubscribed(sessionId) {
			client.SendNotification(method, params)
		}
	}
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
}
