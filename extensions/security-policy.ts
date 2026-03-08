import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";
import path from "node:path";

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

  const blockedEnvCommands = [
    "printenv",
    "env | ",
    "echo $ANTHROPIC",
    "echo $OPENAI",
  ];

  function matchedDangerousPattern(command: string): string | undefined {
    return blockedPatterns.find((pattern) => command.includes(pattern));
  }

  function matchedEnvExfiltrationPattern(command: string): string | undefined {
    return blockedEnvCommands.find((pattern) => command.includes(pattern));
  }

  // Intercept bash tool calls
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;

    const command = (event.input.command ?? "") as string;

    const dangerous = matchedDangerousPattern(command);
    if (dangerous) {
      // Check if UI is available for HITL approval
      if (!ctx.hasUI) {
        return { block: true, reason: `Blocked dangerous pattern "${dangerous}" (no UI available)` };
      }

      // Request human approval with detailed information
      const confirmed = await ctx.ui.confirm(
        "Dangerous Command Detected",
        `Command contains potentially dangerous pattern: "${dangerous}"\n\nFull command:\n${command}\n\nAllow execution?`
      );

      if (!confirmed) {
        return { block: true, reason: `Dangerous command denied by user: "${dangerous}"` };
      }

      return undefined; // Allow after approval
    }

    const envLeak = matchedEnvExfiltrationPattern(command);
    if (envLeak) {
      // Check if UI is available for HITL approval
      if (!ctx.hasUI) {
        return { block: true, reason: "Blocked environment variable access (no UI available)" };
      }

      // Request human approval with detailed information
      const confirmed = await ctx.ui.confirm(
        "Environment Access Detected",
        `Command attempts to access environment variables: "${envLeak}"\n\nFull command:\n${command}\n\nAllow execution?`
      );

      if (!confirmed) {
        return { block: true, reason: `Environment access denied by user: "${envLeak}"` };
      }

      return undefined; // Allow after approval
    }

    return undefined;
  });

  // Intercept write/edit tool calls for path validation
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "write" && event.toolName !== "edit")
      return undefined;

    const filePath = (event.input.path ?? "") as string;
    if (allowedPaths.length > 0 && filePath) {
      const clean = path.resolve(filePath);
      const allowed = allowedPaths.some((ap: string) => {
        const expanded = ap.replace("~", process.env.HOME ?? "");
        return clean.startsWith(path.resolve(expanded));
      });
      if (!allowed) {
        // Check if UI is available for HITL approval
        if (!ctx.hasUI) {
          return {
            block: true,
            reason: `Path "${filePath}" outside allowed paths (no UI available)`,
          };
        }

        // Request human approval with detailed information
        const confirmed = await ctx.ui.confirm(
          "File Path Access",
          `File operation outside allowed paths:\n\nPath: ${filePath}\nTool: ${event.toolName}\n\nAllowed paths: ${allowedPaths.join(", ")}\n\nAllow access?`
        );

        if (!confirmed) {
          return {
            block: true,
            reason: `Path access denied by user: "${filePath}"`,
          };
        }

        return undefined; // Allow after approval
      }
    }

    return undefined;
  });
};

export default securityPolicy;
