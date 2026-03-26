// extensions/memory-tools.ts
// Exposes Add_memory, Update_memory, Delete_memory tools to the pi agent.
// The Go bridge intercepts tool_execution_start events for these tool names
// and routes them to the atomic notes store. The execute() functions here
// only provide the tool result visible to the agent — no stdout writes.

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "@sinclair/typebox";

export default function memoryToolsExtension(pi: ExtensionAPI) {
  pi.registerTool({
    name: "Add_memory",
    label: "Add memory note",
    description:
      "Persist a new atomic memory note. Use when the user states a new preference, reveals a project/tool/workflow pattern, or corrects a prior assumption. One atomic idea per call.",
    parameters: Type.Object({
      content: Type.String({ description: "The specific fact or preference to remember" }),
      keywords: Type.Array(Type.String(), { description: "3-7 lowercase keywords for future retrieval" }),
      tags: Type.Optional(Type.Array(Type.String(), { description: "Category tags (e.g. preference, workflow, project, tool)" })),
    }),
    async execute(_toolCallId, _params) {
      // Actual persistence is handled by the Go bridge intercepting tool_execution_start.
      return { content: [{ type: "text" as const, text: JSON.stringify({ success: true, message: "Memory note added." }) }], details: {} };
    },
  });

  pi.registerTool({
    name: "Update_memory",
    label: "Update memory note",
    description:
      "Update an existing memory note by ID. Use when a previously saved preference has changed. The note ID appears in the Memory Notes section of your context as [note-XXXXXXXX].",
    parameters: Type.Object({
      id: Type.String({ description: "The note ID to update (format: note-XXXXXXXX)" }),
      content: Type.String({ description: "The updated fact or preference" }),
      keywords: Type.Optional(Type.Array(Type.String())),
      tags: Type.Optional(Type.Array(Type.String())),
    }),
    async execute(_toolCallId, _params) {
      return { content: [{ type: "text" as const, text: JSON.stringify({ success: true, message: "Memory note updated." }) }], details: {} };
    },
  });

  pi.registerTool({
    name: "Delete_memory",
    label: "Delete memory note",
    description:
      "Delete a memory note by ID. Use when a saved preference is no longer valid (e.g. user switched frameworks or tools).",
    parameters: Type.Object({
      id: Type.String({ description: "The note ID to delete (format: note-XXXXXXXX)" }),
      reason: Type.Optional(Type.String({ description: "Why this note is being deleted" })),
    }),
    async execute(_toolCallId, _params) {
      return { content: [{ type: "text" as const, text: JSON.stringify({ success: true, message: "Memory note deleted." }) }], details: {} };
    },
  });

  pi.registerTool({
    name: "Retrieve_memory",
    label: "Retrieve memory notes",
    description:
      "Search your memory notes for context relevant to the current subtask. Call this when you need to recall specific preferences, project details, or prior decisions related to what you are working on.",
    parameters: Type.Object({
      query: Type.String({ description: "Natural language description of what you are looking for" }),
      top_k: Type.Optional(Type.Number({ description: "Maximum number of notes to return (default 5)" })),
    }),
    async execute(_toolCallId, _args) {
      const retrievePath = process.env.AGENTLOOP_RETRIEVE_PATH;
      if (!retrievePath) {
        return { content: [{ type: "text" as const, text: JSON.stringify({ notes: [], message: "AGENTLOOP_RETRIEVE_PATH not set" }) }], details: {} };
      }
      try {
        const fs = await import("fs");
        const raw = fs.readFileSync(retrievePath, "utf8");
        const notes = JSON.parse(raw) as Array<{ ID: string; Content: string; Keywords: string[] }>;
        return { content: [{ type: "text" as const, text: JSON.stringify({ notes, count: notes.length }) }], details: {} };
      } catch {
        return { content: [{ type: "text" as const, text: JSON.stringify({ notes: [], message: "no results" }) }], details: {} };
      }
    },
  });
}
