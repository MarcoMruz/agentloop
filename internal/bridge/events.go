package bridge

import "encoding/json"

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
	Method    string          `json:"method,omitempty"`
	Title     string          `json:"title,omitempty"`
	UIMessage json.RawMessage `json:"message,omitempty"` // body text (string) from ctx.ui.confirm(title, message); object in other event types
	Options   []string        `json:"options,omitempty"`
	Timeout   int             `json:"timeout,omitempty"`
}

// MemoryToolEvent is constructed when the bridge intercepts a tool_execution_start
// for Add_memory, Update_memory, Delete_memory, or Retrieve_memory.
type MemoryToolEvent struct {
	Operation string   // "add", "update", "delete", "retrieve"
	NoteID    string   // for update/delete
	Content   string
	Keywords  []string
	Tags      []string
	// Retrieve_memory fields
	Query        string
	TopK         int
	RetrievePath string // temp file path — bridge writes results here
}

// MemoryToolHandler is called synchronously when a memory tool call is intercepted.
type MemoryToolHandler func(event MemoryToolEvent)

// SkillToolEvent is fired when the pi agent calls the Find_skill tool.
type SkillToolEvent struct {
	Tool          string         // "Find_skill"
	Params        map[string]any
	SkillLoadPath string
}

// SkillToolHandler is called when the Find_skill tool is invoked.
type SkillToolHandler func(event SkillToolEvent)

// ExtensionUIResponse is sent back to pi when a dialog extension_ui_request needs a reply.
// UIMessageString extracts UIMessage as a plain string.
// For extension_ui_request events the field is a JSON-encoded string; for
// other event types (message_start, message_end, turn_end …) it is a JSON
// object — this returns "" for those so callers never see a parse error.
func (e RPCEvent) UIMessageString() string {
	if len(e.UIMessage) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.UIMessage, &s); err != nil {
		return ""
	}
	return s
}

type ExtensionUIResponse struct {
	Type      string `json:"type"`             // always "extension_ui_response"
	ID        string `json:"id"`               // matches the request ID
	Value     string `json:"value,omitempty"`  // for select/input/editor
	Confirmed *bool  `json:"confirmed,omitempty"` // for confirm
	Cancelled bool   `json:"cancelled,omitempty"` // cancels any dialog
}
