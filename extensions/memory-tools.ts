// extensions/memory-tools.ts
// Exposes Add_memory, Update_memory, Delete_memory tools to the pi agent.
// The Go bridge intercepts tool_execution_start events for these tool names
// and routes them to the atomic notes store. The execute() functions here
// only provide the tool result visible to the agent — no stdout writes.

import { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const factory: ExtensionFactory = (pi) => {
  pi.addTool({
    name: "Add_memory",
    description:
      "Persist a new atomic memory note. Use when the user states a new preference, reveals a project/tool/workflow pattern, or corrects a prior assumption. One atomic idea per call.",
    parameters: {
      type: "object",
      properties: {
        content: {
          type: "string",
          description: "The specific fact or preference to remember",
        },
        keywords: {
          type: "array",
          items: { type: "string" },
          description: "3-7 lowercase keywords for future retrieval",
        },
        tags: {
          type: "array",
          items: { type: "string" },
          description: "Category tags (e.g. preference, workflow, project, tool)",
        },
      },
      required: ["content", "keywords"],
    },
    execute: async (_params) => {
      // Actual persistence is handled by the Go bridge intercepting tool_execution_start.
      return { success: true, message: "Memory note added." };
    },
  });

  pi.addTool({
    name: "Update_memory",
    description:
      "Update an existing memory note by ID. Use when a previously saved preference has changed. The note ID appears in the Memory Notes section of your context as [note-XXXXXXXX].",
    parameters: {
      type: "object",
      properties: {
        id: {
          type: "string",
          description: "The note ID to update (format: note-XXXXXXXX)",
        },
        content: {
          type: "string",
          description: "The updated fact or preference",
        },
        keywords: {
          type: "array",
          items: { type: "string" },
        },
        tags: {
          type: "array",
          items: { type: "string" },
        },
      },
      required: ["id", "content"],
    },
    execute: async (_params) => {
      return { success: true, message: "Memory note updated." };
    },
  });

  pi.addTool({
    name: "Delete_memory",
    description:
      "Delete a memory note by ID. Use when a saved preference is no longer valid (e.g. user switched frameworks or tools).",
    parameters: {
      type: "object",
      properties: {
        id: {
          type: "string",
          description: "The note ID to delete (format: note-XXXXXXXX)",
        },
        reason: {
          type: "string",
          description: "Why this note is being deleted",
        },
      },
      required: ["id"],
    },
    execute: async (_params) => {
      return { success: true, message: "Memory note deleted." };
    },
  });
};

export default factory;
