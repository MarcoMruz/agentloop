import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const dockerGuard: ExtensionFactory = (pi) => {
  const allowedSubs = (process.env.AGENTLOOP_DOCKER_ALLOWED || "ps,logs,images,build,compose,inspect,stats")
    .split(",").filter(Boolean);
  const blockedVolPaths = (process.env.AGENTLOOP_DOCKER_BLOCKED_VOLS || "/etc,/var,/root,/proc,/sys,/dev")
    .split(",").filter(Boolean);

  pi.on("toolCall", async (event, ctx) => {
    if (event.tool.name !== "bash") return { action: "continue" };
    const cmd = event.input?.command || "";
    if (!cmd.includes("docker")) return { action: "continue" };

    // Extract subcommand
    const words = cmd.split(/\s+/);
    const dockerIdx = words.findIndex((w: string) => w === "docker" || w === "docker-compose");
    if (dockerIdx === -1) return { action: "continue" };
    const subcmd = words[dockerIdx + 1] || "";

    if (allowedSubs.length > 0 && !allowedSubs.includes(subcmd)) {
      return { action: "block", message: `Docker subcommand "${subcmd}" not allowed` };
    }

    // Check volume mounts
    for (let i = 0; i < words.length; i++) {
      if ((words[i] === "-v" || words[i] === "--volume") && words[i + 1]) {
        const hostPath = words[i + 1].split(":")[0];
        for (const bp of blockedVolPaths) {
          if (hostPath.startsWith(bp)) {
            return { action: "block", message: `Volume mount to "${hostPath}" blocked` };
          }
        }
      }
    }

    return { action: "continue" };
  });
};

export default dockerGuard;
