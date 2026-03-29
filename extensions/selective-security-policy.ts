import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";
import path from "node:path";

const selectiveSecurityPolicy: ExtensionFactory = (pi) => {
  // Read security config from environment (set by Go orchestrator)
  const allowedPaths = (process.env.AGENTLOOP_ALLOWED_PATHS || "")
    .split(",")
    .filter(Boolean);
  const policyMode = process.env.AGENTLOOP_POLICY_MODE || "selective";
  
  // Tiered operation patterns from Go config
  const safePatterns = (process.env.AGENTLOOP_SAFE_PATTERNS || "")
    .split(",")
    .filter(Boolean);
  const loggedPatterns = (process.env.AGENTLOOP_LOGGED_PATTERNS || "")
    .split(",")
    .filter(Boolean);
  const hitlPatterns = (process.env.AGENTLOOP_HITL_PATTERNS || "")
    .split(",")
    .filter(Boolean);
  const blockedPatterns = (process.env.AGENTLOOP_BLOCKED_PATTERNS || "")
    .split(",")
    .filter(Boolean);

  // Default safe patterns if not configured
  const defaultSafePatterns = [
    "^ls\\b",
    "^cat\\b", 
    "^grep\\b",
    "^find\\b",
    "^pwd$",
    "^echo\\b",
    "^which\\b",
    "^git log",
    "^git status",
    "^git diff",
    "^head\\b",
    "^tail\\b",
    "^wc\\b",
  ];

  const defaultLoggedPatterns = [
    "^git checkout",
    "^git branch",
    "^npm install",
    "^yarn install", 
    "^mkdir\\b",
    "^touch\\b",
    "^cp\\b",
    "^mv\\b",
  ];

  const defaultHITLPatterns = [
    "\\bsudo\\b",
    "\\bchmod\\b", 
    "\\bchown\\b",
    "\\brm\\b.*-r",
    "\\bsystemctl\\b",
    "curl.*-X.*POST",
  ];

  const defaultBlockedPatterns = [
    "rm -rf /",
    "mkfs",
    "> /dev/sd",
    "dd if=",
    ":(){ :|:& };:",
    "shutdown",
    "reboot",
  ];

  // Use configured patterns or defaults
  const finalSafePatterns = safePatterns.length > 0 ? safePatterns : defaultSafePatterns;
  const finalLoggedPatterns = loggedPatterns.length > 0 ? loggedPatterns : defaultLoggedPatterns;
  const finalHITLPatterns = hitlPatterns.length > 0 ? hitlPatterns : defaultHITLPatterns;
  const finalBlockedPatterns = blockedPatterns.length > 0 ? blockedPatterns : defaultBlockedPatterns;

  function classifyBashCommand(command: string): "allow" | "log" | "hitl" | "block" {
    // If not in selective mode, default to HITL for everything
    if (policyMode !== "selective") {
      return "hitl";
    }

    // Check blocked patterns first (highest priority)
    const blockedMatch = finalBlockedPatterns.find(pattern => {
      try {
        return new RegExp(pattern).test(command);
      } catch {
        return command.includes(pattern);
      }
    });
    if (blockedMatch) {
      return "block";
    }

    // Check HITL required patterns
    const hitlMatch = finalHITLPatterns.find(pattern => {
      try {
        return new RegExp(pattern).test(command);
      } catch {
        return command.includes(pattern);
      }
    });
    if (hitlMatch) {
      return "hitl";
    }

    // Check logged patterns  
    const loggedMatch = finalLoggedPatterns.find(pattern => {
      try {
        return new RegExp(pattern).test(command);
      } catch {
        return command.includes(pattern);
      }
    });
    if (loggedMatch) {
      return "log";
    }

    // Check safe patterns
    const safeMatch = finalSafePatterns.find(pattern => {
      try {
        return new RegExp(pattern).test(command);
      } catch {
        return command.includes(pattern);
      }
    });
    if (safeMatch) {
      return "allow";
    }

    // Handle docker commands separately
    if (command.includes("docker")) {
      return classifyDockerCommand(command);
    }

    // Unknown commands default to HITL for safety
    return "hitl";
  }

  function classifyDockerCommand(command: string): "allow" | "log" | "hitl" | "block" {
    const words = command.split(/\s+/);
    const dockerIdx = words.findIndex(w => w === "docker" || w === "docker-compose");
    
    if (dockerIdx === -1 || dockerIdx + 1 >= words.length) {
      return "hitl";
    }
    
    const subcmd = words[dockerIdx + 1];

    // Check for dangerous volume mounts (always blocked)
    if (command.includes("-v") || command.includes("--volume")) {
      const blockedVolumes = ["/etc", "/var", "/root", "/proc", "/sys", "/dev"];
      for (let i = 0; i < words.length; i++) {
        if ((words[i] === "-v" || words[i] === "--volume") && words[i + 1]) {
          const hostPath = words[i + 1].split(":")[0];
          if (blockedVolumes.some(blocked => hostPath.startsWith(blocked))) {
            return "block";
          }
        }
      }
    }

    const safeCmds = ["ps", "logs", "images", "inspect", "stats", "top"];
    const loggedCmds = ["build", "run", "exec", "start"];
    const hitlCmds = ["rm", "stop", "restart", "compose"];

    if (safeCmds.includes(subcmd)) return "allow";
    if (loggedCmds.includes(subcmd)) return "log";
    if (hitlCmds.includes(subcmd)) return "hitl";
    
    return "hitl"; // Unknown docker commands default to HITL
  }

  function classifyFileOperation(toolName: string, filePath: string): "allow" | "log" | "hitl" {
    // Check if path is outside allowed paths
    if (allowedPaths.length > 0 && filePath) {
      const resolvedPath = path.resolve(filePath);
      const pathAllowed = allowedPaths.some(allowedPath => {
        const expanded = allowedPath.replace("~", process.env.HOME ?? "");
        return resolvedPath.startsWith(path.resolve(expanded));
      });
      
      if (!pathAllowed) {
        return "hitl"; // Require approval for paths outside allowed dirs
      }
    }

    // Check for sensitive locations
    const sensitivePatterns = [
      "/etc/", "/var/", "/root/", "/proc/", "/sys/", "/dev/",
      "/.git/", "/node_modules/", ".env", "credentials", "config"
    ];

    const isSensitive = sensitivePatterns.some(pattern => 
      filePath.toLowerCase().includes(pattern)
    );

    if (isSensitive) {
      return "hitl";
    }

    // File operations in allowed paths are logged
    return "log";
  }

  function classifyReadOperation(filePath: string): "allow" | "log" {
    // Check for sensitive files
    const sensitivePatterns = [
      ".env", "credentials", "config", "password", "secret", "key",
      "/etc/passwd", "/etc/shadow", "id_rsa", "private"
    ];

    const isSensitive = sensitivePatterns.some(pattern => 
      filePath.toLowerCase().includes(pattern)
    );

    return isSensitive ? "log" : "allow";
  }

  // Intercept bash tool calls with tiered security
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;

    const command = (event.input.command ?? "") as string;
    const classification = classifyBashCommand(command);

    switch (classification) {
      case "allow":
        // Log safe operations for audit trail
        console.error(`[SECURITY] ALLOWED: ${command}`);
        return undefined; // Allow execution

      case "log":
        // Log moderate risk operations but allow them
        console.error(`[SECURITY] LOGGED: ${command}`);
        return undefined; // Allow execution

      case "hitl":
        // Require human approval for high risk operations
        if (!ctx.hasUI) {
          return { 
            block: true, 
            reason: `High-risk command requires approval (no UI available): ${command}` 
          };
        }

        console.error(`[SECURITY] HITL REQUIRED: ${command}`);
        const confirmed = await ctx.ui.confirm(
          "High-Risk Command Approval",
          `Command requires approval:\n\n${command}\n\nThis command has been classified as high-risk. Allow execution?`
        );

        if (!confirmed) {
          return { block: true, reason: `High-risk command denied by user: ${command}` };
        }

        console.error(`[SECURITY] HITL APPROVED: ${command}`);
        return undefined; // Allow after approval

      case "block":
        // Always block dangerous operations
        console.error(`[SECURITY] BLOCKED: ${command}`);
        return { 
          block: true, 
          reason: `Dangerous command blocked by security policy: ${command}` 
        };

      default:
        return { block: true, reason: "Unknown security classification" };
    }
  });

  // Intercept write/edit tool calls with tiered security
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "write" && event.toolName !== "edit") {
      return undefined;
    }

    const filePath = (event.input.path ?? "") as string;
    const classification = classifyFileOperation(event.toolName, filePath);

    switch (classification) {
      case "allow":
        console.error(`[SECURITY] ALLOWED: ${event.toolName} ${filePath}`);
        return undefined;

      case "log":
        console.error(`[SECURITY] LOGGED: ${event.toolName} ${filePath}`);
        return undefined;

      case "hitl":
        if (!ctx.hasUI) {
          return {
            block: true,
            reason: `File operation requires approval (no UI available): ${event.toolName} ${filePath}`
          };
        }

        console.error(`[SECURITY] HITL REQUIRED: ${event.toolName} ${filePath}`);
        const confirmed = await ctx.ui.confirm(
          "File Operation Approval",
          `File operation requires approval:\n\nTool: ${event.toolName}\nPath: ${filePath}\n\nAllow access?`
        );

        if (!confirmed) {
          return {
            block: true,
            reason: `File operation denied by user: ${event.toolName} ${filePath}`
          };
        }

        console.error(`[SECURITY] HITL APPROVED: ${event.toolName} ${filePath}`);
        return undefined;

      default:
        return { block: true, reason: "Unknown security classification" };
    }
  });

  // Intercept read tool calls (mostly allowed, some logged)
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "read") return undefined;

    const filePath = (event.input.path ?? "") as string;
    const classification = classifyReadOperation(filePath);

    if (classification === "log") {
      console.error(`[SECURITY] SENSITIVE READ: ${filePath}`);
    } else {
      console.error(`[SECURITY] READ: ${filePath}`);
    }

    return undefined; // Always allow reads, just log sensitive ones
  });
};

export default selectiveSecurityPolicy;