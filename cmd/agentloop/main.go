package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var socketPath = os.Getenv("AGENTLOOP_SOCKET")

func init() {
	if socketPath == "" {
		home, _ := os.UserHomeDir()
		socketPath = home + "/.local/share/agentloop/agentloop.sock"
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: agentloop <task description>")
		fmt.Println("       agentloop --status")
		fmt.Println("       agentloop --abort <sessionId>")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to AgentLoop server at %s\nIs agentloop-server running?\n", socketPath)
		os.Exit(1)
	}
	defer conn.Close()

	task := strings.Join(os.Args[1:], " ")

	// Send task.start
	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "task.start",
		"params": map[string]any{
			"userId": os.Getenv("USER"),
			"text":   task,
			"source": "cli",
		},
	}
	data, _ := json.Marshal(req)
	fmt.Fprintf(conn, "%s\n", data)

	// Read events
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		var msg map[string]any
		json.Unmarshal([]byte(scanner.Text()), &msg)

		method, _ := msg["method"].(string)
		params, _ := msg["params"].(map[string]any)

		switch method {
		case "event.text":
			fmt.Print(params["content"])
		case "event.tool_use":
			fmt.Printf("\n> %s\n", params["toolName"])
		case "event.hitl_request":
			fmt.Printf("\n  HITL: %s\nApprove? [y/n/abort]: ", params["details"])
			var input string
			fmt.Scanln(&input)
			decision := "deny"
			switch strings.ToLower(strings.TrimSpace(input)) {
			case "y", "yes", "approve": decision = "approve"
			case "abort": decision = "abort"
			}
			resp := map[string]any{
				"jsonrpc": "2.0", "id": 2, "method": "hitl.respond",
				"params": map[string]any{
					"sessionId": params["sessionId"],
					"requestId": params["requestId"],
					"decision":  decision,
				},
			}
			d, _ := json.Marshal(resp)
			fmt.Fprintf(conn, "%s\n", d)
		case "event.done":
			fmt.Printf("\n\nDone (%s)\n", params["stats"])
			return
		case "event.error":
			fmt.Printf("\nError: %s\n", params["message"])
			return
		default:
			// result response for task.start
			if result, ok := msg["result"].(map[string]any); ok {
				if sid, ok := result["sessionId"].(string); ok {
					_ = sid // subscribed automatically
				}
			}
		}
	}

	_ = ctx // for future Ctrl+C handling
}
