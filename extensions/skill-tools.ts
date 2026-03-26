// extensions/skill-tools.ts
// Exposes Find_skill tool to the pi agent for LLM-driven skill selection.
// The Go bridge intercepts tool_execution_start events for this tool name
// and routes them to the skill finder. The execute() function here
// reads the already-written result from a temp file (IPC contract).

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "@sinclair/typebox";

export default function skillToolsExtension(pi: ExtensionAPI) {
  pi.registerTool({
    name: "Find_skill",
    label: "Find skill",
    description:
      "Find and load the most relevant skill for your current task. Describe what you need in natural language — a side agent will search the skill catalog and return the best match with full instructions and available files. Call this when you need specialized workflow instructions for a domain like deployment, testing, migrations, etc.",
    parameters: Type.Object({
      query: Type.String({ description: "Natural language description of the task or domain you need skill instructions for." }),
    }),
    async execute(_toolCallId, _params) {
      const loadPath = process.env.AGENTLOOP_SKILL_LOAD_PATH;
      if (!loadPath) {
        return { content: [{ type: "text" as const, text: JSON.stringify({ error: "AGENTLOOP_SKILL_LOAD_PATH not set" }) }], details: {} };
      }
      try {
        const fs = await import("fs");
        const data = fs.readFileSync(loadPath, "utf-8");
        return { content: [{ type: "text" as const, text: data }], details: {} };
      } catch (e: any) {
        return { content: [{ type: "text" as const, text: JSON.stringify({ error: "failed to read skill result: " + (e?.message ?? String(e)) }) }], details: {} };
      }
    },
  });
}
