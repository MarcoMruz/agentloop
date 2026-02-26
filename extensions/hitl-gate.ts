import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const hitlGate: ExtensionFactory = (pi) => {
  // Tools that always require approval
  const alwaysApprove = (process.env.AGENTLOOP_HITL_TOOLS || "docker,n8n_webhook")
    .split(",")
    .filter(Boolean);

  // Patterns in bash commands that trigger HITL
  const riskyPatterns = [
    "docker",
    "curl",
    "wget",
    "npm publish",
    "git push",
    "rm -r",
    "sudo",
  ];

  pi.on("toolCall", async (event, ctx) => {
    const toolName = event.tool.name;

    // Check if this tool always needs approval
    if (alwaysApprove.includes(toolName)) {
      const approved = await ctx.ui.confirm(
        `AgentLoop HITL: Allow ${toolName}?`,
        `Command: ${JSON.stringify(event.input).slice(0, 200)}`
      );
      if (!approved) {
        return { action: "block", message: "Blocked by HITL gate" };
      }
      return { action: "continue" };
    }

    // Check bash commands for risky patterns
    if (toolName === "bash") {
      const command = event.input?.command || "";
      const risky = riskyPatterns.some((p) => command.includes(p));
      if (risky) {
        const approved = await ctx.ui.confirm(
          `AgentLoop HITL: Allow bash command?`,
          `$ ${command.slice(0, 300)}`
        );
        if (!approved) {
          return { action: "block", message: "Blocked by HITL gate" };
        }
      }
    }

    return { action: "continue" };
  });
};

export default hitlGate;
