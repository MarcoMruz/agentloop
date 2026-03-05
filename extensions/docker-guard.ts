import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const dockerGuard: ExtensionFactory = (pi) => {
  const allowedSubs = (process.env.AGENTLOOP_DOCKER_ALLOWED || "ps,logs,images,build,compose,inspect,stats")
    .split(",").filter(Boolean);
  const blockedVolPaths = (process.env.AGENTLOOP_DOCKER_BLOCKED_VOLS || "/etc,/var,/root,/proc,/sys,/dev")
    .split(",").filter(Boolean);

  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;
    const cmd = event.input.command as string;
    if (!cmd.includes("docker")) return undefined;

    // Extract subcommand
    const words = cmd.split(/\s+/);
    const dockerIdx = words.findIndex((w: string) => w === "docker" || w === "docker-compose");
    if (dockerIdx === -1) return undefined;
    const subcmd = words[dockerIdx + 1] || "";

    // Block disallowed subcommands immediately (no HITL — policy violation)
    if (allowedSubs.length > 0 && !allowedSubs.includes(subcmd)) {
      return { block: true, reason: `Docker subcommand "${subcmd}" not allowed` };
    }

    // Block dangerous volume mounts immediately (no HITL — policy violation)
    for (let i = 0; i < words.length; i++) {
      if ((words[i] === "-v" || words[i] === "--volume") && words[i + 1]) {
        const hostPath = words[i + 1].split(":")[0];
        for (const bp of blockedVolPaths) {
          if (hostPath.startsWith(bp)) {
            return { block: true, reason: `Volume mount to "${hostPath}" blocked` };
          }
        }
      }
    }

    // Allowed subcommand — still require explicit human approval via HITL
    if (!ctx.hasUI) {
      // RPC/print mode with no UI: block by default for safety
      return { block: true, reason: "Docker command blocked (no UI available for confirmation)" };
    }

    const confirmed = await ctx.ui.confirm(
      "Docker Command Approval Required",
      `Docker command requires approval:\n\n${cmd}`
    );

    if (!confirmed) {
      return { block: true, reason: "Docker command denied by user" };
    }

    return undefined;
  });
};

export default dockerGuard;
