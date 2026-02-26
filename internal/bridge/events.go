package bridge

// RPCCommand is sent from Go to pi via stdin.
type RPCCommand struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Text string `json:"text,omitempty"`
}

// RPCEvent is received from pi via stdout.
type RPCEvent struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Content string `json:"content,omitempty"`

	// tool_use / tool_result events
	Name   string         `json:"name,omitempty"`
	Input  map[string]any `json:"input,omitempty"`
	Output string         `json:"output,omitempty"`

	// error events
	Message string `json:"message,omitempty"`

	// extension_ui_request events (HITL gate uses these)
	Method  string   `json:"method,omitempty"`
	Title   string   `json:"title,omitempty"`
	Options []string `json:"options,omitempty"`
	Timeout int      `json:"timeout,omitempty"`
}

// ExtensionUIResponse is sent back to pi when an extension_ui_request needs a reply.
type ExtensionUIResponse struct {
	Type      string `json:"type"`      // always "extension_ui_response"
	ID        string `json:"id"`        // matches the request ID
	Value     string `json:"value,omitempty"`
	Confirmed *bool  `json:"confirmed,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}
