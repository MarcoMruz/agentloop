// extensions/feedback-tools.ts
// Exposes the Submit_feedback tool to the pi agent.
// The Go bridge intercepts tool_execution_start events for this tool name
// and routes them to the feedback pipeline (MemEvolve Collector).
// The execute() function here only returns the stub result visible to the agent.

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "@sinclair/typebox";

export default function feedbackToolsExtension(pi: ExtensionAPI) {
  pi.registerTool({
    name: "Submit_feedback",
    label: "Submit user feedback",
    description:
      "Submit explicit user feedback about a prior task or agent response. " +
      "Call this tool whenever the user expresses dissatisfaction, reports incorrect or unexpected results, " +
      "describes unwanted behavior, or explicitly asks to improve the agent. " +
      "Do NOT call this for neutral observations or requests for new tasks.",
    parameters: Type.Object({
      text: Type.String({
        description: "The user's feedback verbatim or a faithful paraphrase of their dissatisfaction",
      }),
      context: Type.Optional(
        Type.String({
          description: "What went wrong — a brief description of the incorrect or unexpected outcome",
        })
      ),
      expected_behavior: Type.Optional(
        Type.String({
          description: "What the user expected to happen instead",
        })
      ),
    }),
    async execute(_toolCallId, _params) {
      // Actual processing is handled by the Go bridge intercepting tool_execution_start.
      return {
        content: [{ type: "text" as const, text: JSON.stringify({ success: true, message: "Feedback submitted." }) }],
        details: {},
      };
    },
  });
}
