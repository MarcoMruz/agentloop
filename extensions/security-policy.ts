import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const securityPolicy: ExtensionFactory = (pi) => {
  // Read security config from environment (set by Go orchestrator)
  const allowedPaths = (process.env.AGENTLOOP_ALLOWED_PATHS || "")
    .split(",")
    .filter(Boolean);

  const blockedPatterns = [
    "rm -rf /",
    "sudo rm",
    "mkfs",
    "> /dev/sd",
    "dd if=",
    ":(){ :|:& };:",
  ];

  const blockedEnvCommands = ["printenv", "env | ", "echo $ANTHROPIC", "echo $OPENAI"];

  // Intercept bash tool calls
  pi.on("toolCall", async (event, ctx) => {
    if (event.tool.name !== "bash") return { action: "continue" };

    const command = event.input?.command || event.input?.content || "";

    // Block dangerous commands
    for (const pattern of blockedPatterns) {
      if (command.includes(pattern)) {
        return {
          action: "block",
          message: `Blocked dangerous command pattern: "${pattern}"`,
        };
      }
    }

    // Block env exfiltration attempts
    for (const pattern of blockedEnvCommands) {
      if (command.includes(pattern)) {
        return {
          action: "block",
          message: `Blocked environment variable access attempt`,
        };
      }
    }

    return { action: "continue" };
  });

  // Intercept write/edit tool calls for path validation
  for (const toolName of ["write", "edit"]) {
    pi.on("toolCall", async (event, ctx) => {
      if (event.tool.name !== toolName) return { action: "continue" };

      const filePath = event.input?.file_path || event.input?.path || "";
      if (allowedPaths.length > 0 && filePath) {
        const path = require("path");
        const clean = path.resolve(filePath);
        const allowed = allowedPaths.some((ap: string) => {
          const expanded = ap.replace("~", process.env.HOME || "");
          return clean.startsWith(path.resolve(expanded));
        });
        if (!allowed) {
          return {
            action: "block",
            message: `Path "${filePath}" is outside allowed paths`,
          };
        }
      }
      return { action: "continue" };
    });
  }
};

export default securityPolicy;
