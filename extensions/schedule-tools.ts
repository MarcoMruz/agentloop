// extensions/schedule-tools.ts
// Exposes the Schedule_task tool to the pi agent.
// The Go bridge intercepts tool_execution_start events for this tool name
// and routes them to the scheduled task store.
// The execute() function here only returns the stub result visible to the agent.

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "@sinclair/typebox";

export default function scheduleToolsExtension(pi: ExtensionAPI) {
  pi.registerTool({
    name: "Schedule_task",
    label: "Schedule a recurring task",
    description:
      "Schedule a task to run on a recurring basis via the heartbeat system. " +
      "Specify the task name, cron schedule, and the prompt to execute. " +
      "The task will run automatically at the scheduled times.",
    parameters: Type.Object({
      name: Type.String({
        description: "Name of the scheduled task (unique identifier)",
      }),
      schedule: Type.String({
        description: "Cron expression for the schedule (e.g. '0 9 * * *' for 9 AM daily)",
      }),
      description: Type.String({
        description: "Human-readable description of what this task does",
      }),
      prompt: Type.Optional(
        Type.String({
          description: "The prompt/instructions to execute when the task runs",
        })
      ),
    }),
    async execute(_toolCallId, _params) {
      // Actual processing is handled by the Go bridge intercepting tool_execution_start.
      return {
        content: [{ type: "text" as const, text: JSON.stringify({ success: true, message: "Task scheduled." }) }],
        details: {},
      };
    },
  });
}
