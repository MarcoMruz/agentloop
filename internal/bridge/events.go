package bridge

// RPCCommand is sent from Go to pi via stdin.
type RPCCommand struct {
	Type              string `json:"type"`
	ID                string `json:"id,omitempty"`
	Message           string `json:"message,omitempty"`
	StreamingBehavior string `json:"streamingBehavior,omitempty"`
}

// AssistantMessageEvent is the streaming delta within a message_update event.
type AssistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"` // text_delta, thinking_delta
}

// ToolResultContent is a single content block in a tool result.
type ToolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult contains the output of a completed tool execution.
type ToolResult struct {
	Content []ToolResultContent `json:"content"`
}

// RPCEvent is received from pi via stdout.
type RPCEvent struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	// message_update
	AssistantMessageEvent *AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// tool_execution_start / tool_execution_end
	ToolCallId string         `json:"toolCallId,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Result     *ToolResult    `json:"result,omitempty"`
	IsError    bool           `json:"isError,omitempty"`

	// auto_retry_end
	Success    bool   `json:"success,omitempty"`
	FinalError string `json:"finalError,omitempty"`

	// extension_ui_request (HITL gate uses these)
	Method      string   `json:"method,omitempty"`
	Title       string   `json:"title,omitempty"`
	UIMessage   string   `json:"message,omitempty"` // body text from ctx.ui.confirm(title, message)
	Options     []string `json:"options,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
}

// MemoryToolEvent is constructed when the bridge intercepts a tool_execution_start
// for Add_memory, Update_memory, or Delete_memory.
type MemoryToolEvent struct {
	Operation string   // "add", "update", "delete"
	NoteID    string   // for update/delete
	Content   string
	Keywords  []string
	Tags      []string
}

// MemoryToolHandler is called synchronously when a memory tool call is intercepted.
type MemoryToolHandler func(event MemoryToolEvent)

// ExtensionUIResponse is sent back to pi when a dialog extension_ui_request needs a reply.
type ExtensionUIResponse struct {
	Type      string `json:"type"`             // always "extension_ui_response"
	ID        string `json:"id"`               // matches the request ID
	Value     string `json:"value,omitempty"`  // for select/input/editor
	Confirmed *bool  `json:"confirmed,omitempty"` // for confirm
	Cancelled bool   `json:"cancelled,omitempty"` // cancels any dialog
}
