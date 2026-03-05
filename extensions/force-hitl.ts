import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const forceHitl: ExtensionFactory = (pi) => {
  // Keywords loaded from agentloop.yaml → hitl.force_hitl_keywords
  // Injected by the Go orchestrator as AGENTLOOP_HITL_FORCE_KEYWORDS (comma-separated).
  const forceApprovalKeywords = (process.env.AGENTLOOP_HITL_FORCE_KEYWORDS || "")
    .split(",")
    .map((k) => k.trim())
    .filter(Boolean);

  if (forceApprovalKeywords.length === 0) return;

  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;

    const command = event.input.command as string;

    for (const keyword of forceApprovalKeywords) {
      if (command.toLowerCase().includes(keyword.toLowerCase())) {
        if (!ctx.hasUI) {
          // RPC/print mode with no UI: block by default for safety
          return { block: true, reason: `Command with "${keyword}" blocked (no UI available for confirmation)` };
        }

        const confirmed = await ctx.ui.confirm(
          "System Command Approval Required",
          `Command contains "${keyword}" — requires approval:\n\n${command}`
        );

        if (!confirmed) {
          return { block: true, reason: `Command with "${keyword}" denied by user` };
        }

        return undefined;
      }
    }

    return undefined;
  });
};

export default forceHitl;
