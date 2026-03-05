import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const promptInjectionGuard: ExtensionFactory = (pi) => {
  // Read configuration from environment (set by Go orchestrator)
  const enableProtection =
    process.env.AGENTLOOP_INJECTION_PROTECTION !== "false";
  const whitelistSources = (process.env.AGENTLOOP_WHITELIST_SOURCES || "")
    .split(",")
    .filter(Boolean);
  const blockedKeywords = (
    process.env.AGENTLOOP_BLOCKED_KEYWORDS ||
    "ignore previous instructions,forget everything above,act as if you are,pretend you are,roleplay as,API_KEY,SECRET,TOKEN,PASSWORD"
  )
    .split(",")
    .filter(Boolean);
  const requireApprovalPatterns = (
    process.env.AGENTLOOP_REQUIRE_APPROVAL ||
    "skills/*,node_modules/*,*/attachments/*,cloud:*,fetch:*,.git/*"
  )
    .split(",")
    .filter(Boolean);
  const maxContentLength = parseInt(
    process.env.AGENTLOOP_MAX_CONTENT_LENGTH || "50000",
  );

  if (!enableProtection) {
    return; // Protection disabled — early exit, no handlers registered
  }

  // ---------------------------------------------------------------------------
  // Detection helpers
  // ---------------------------------------------------------------------------

  const sensitivePatterns = [
    /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b/g, // emails
    /\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b/g, // IP addresses
    /\b[A-Fa-f0-9]{32,}\b/g, // hex keys
    /sk-[a-zA-Z0-9]{48}/g, // OpenAI keys
    /xoxb-[0-9]+-[0-9]+-[0-9]+-[a-zA-Z0-9]+/g, // Slack tokens
    /bearer\s+[a-zA-Z0-9._-]+/gi, // Bearer tokens
    /basic\s+[a-zA-Z0-9+/=]+/gi, // Basic auth
  ];

  const injectionTriggers = [
    /ignore\s+(previous|all|earlier)\s+instructions/gi,
    /forget\s+(everything|all)\s+(above|before)/gi,
    /act\s+as\s+if\s+you\s+(are|were)/gi,
    /pretend\s+(you\s+are|to\s+be)/gi,
    /roleplay\s+as/gi,
    /you\s+are\s+now\s+a/gi,
    /new\s+instructions:/gi,
    /system\s*:\s*/gi,
    /assistant\s*:\s*/gi,
    /\[INST\]/gi,
    /<\|im_start\|>/gi,
  ];

  const dangerousCommandPatterns = [
    /curl\s+.*\|\s*bash/gi, // curl piped to bash
    /wget\s+.*\|\s*sh/gi, // wget piped to shell
    /eval\s*\$\(/gi, // eval with command substitution
    /exec\s*\$\(/gi, // exec with command substitution
    /rm\s+-rf\s+\/\w/gi, // dangerous rm commands
    /chmod\s+777/gi, // overly permissive chmod
    /mkfs/gi, // filesystem creation
    /dd\s+if=/gi, // disk operations
  ];

  function detectSourceFromPath(path: string): string {
    if (!path) return "unknown";
    const cleanPath = path.toLowerCase();
    if (cleanPath.includes("node_modules")) return "node_modules";
    if (cleanPath.includes("/skills/") || cleanPath.includes("\\skills\\"))
      return "skills";
    if (
      cleanPath.includes("/attachments/") ||
      cleanPath.includes("\\attachments\\")
    )
      return "email_attachments";
    if (cleanPath.includes("/.git/") || cleanPath.includes("\\.git\\"))
      return "git_repo";
    if (path.startsWith("cloud:")) return "cloud_file";
    if (path.startsWith("fetch:")) return "fetch_response";
    return "user_input";
  }

  function analyzeInjectionRisk(
    content: string,
    source: string,
  ): { risk: string; triggers: string[] } {
    const triggers: string[] = [];
    let riskLevel = "low";

    if (!content) return { risk: riskLevel, triggers };

    if (content.length > maxContentLength) {
      triggers.push("content_too_large");
      riskLevel = "high";
    }

    const lowerContent = content.toLowerCase();
    for (const keyword of blockedKeywords) {
      if (lowerContent.includes(keyword.toLowerCase())) {
        triggers.push(`blocked_keyword:${keyword}`);
        if (riskLevel === "low") riskLevel = "medium";
      }
    }

    for (const pattern of injectionTriggers) {
      if (pattern.test(content)) {
        triggers.push(`injection_pattern:${pattern.source}`);
        riskLevel = "high";
      }
    }

    for (const pattern of sensitivePatterns) {
      if (pattern.test(content)) {
        triggers.push(`sensitive_data:${pattern.source}`);
        riskLevel = "high";
      }
    }

    switch (source) {
      case "skills":
      case "fetch_response":
      case "git_repo":
        if (riskLevel === "low") riskLevel = "medium";
        break;
      case "node_modules":
      case "email_attachments":
      case "cloud_file":
        if (riskLevel === "low") riskLevel = "high";
        break;
    }

    return { risk: riskLevel, triggers };
  }

  function requiresApproval(path: string, source: string): boolean {
    if (!path) return false;
    for (const pattern of requireApprovalPatterns) {
      if (pattern.includes("*")) {
        const regex = new RegExp(pattern.replace(/\*/g, ".*"));
        if (regex.test(path)) return true;
      } else if (path.includes(pattern)) {
        return true;
      }
    }
    return ["node_modules", "email_attachments", "cloud_file"].includes(source);
  }

  // ---------------------------------------------------------------------------
  // Handlers
  // ---------------------------------------------------------------------------

  // read — whitelist check + high-risk source gate
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "read") return undefined;

    const filePath = (event.input.path ?? "") as string;
    const source = detectSourceFromPath(filePath);

    // Whitelist gate: if whitelist is configured and file is outside it → confirm
    if (whitelistSources.length > 0 && filePath) {
      const isWhitelisted = whitelistSources.some((allowed) => {
        const expanded = allowed.replace("~", process.env.HOME ?? "");
        return filePath.startsWith(expanded);
      });
      if (!isWhitelisted) {
        if (!ctx.hasUI) {
          return {
            block: true,
            reason: `File "${filePath}" is outside whitelisted paths`,
          };
        }
        const confirmed = await ctx.ui.confirm(
          "Non-whitelisted File Access",
          `File is outside whitelisted paths.\nPath: ${filePath}\nSource: ${source}`,
        );
        if (!confirmed) {
          return { block: true, reason: "File access denied by user" };
        }
      }
    }

    // High-risk source gate
    if (requiresApproval(filePath, source)) {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: `High-risk file access blocked (source: ${source})`,
        };
      }
      const confirmed = await ctx.ui.confirm(
        "High-Risk File Access",
        `Reading from a potentially dangerous source.\nSource: ${source}\nPath: ${filePath}`,
      );
      if (!confirmed) {
        return { block: true, reason: "High-risk file access denied by user" };
      }
    }

    return undefined;
  });

  // bash — dangerous command patterns + injection risk analysis
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;

    const command = (event.input.command ?? "") as string;
    if (!command) return undefined;

    const analysis = analyzeInjectionRisk(command, "bash_command");

    // Escalate to critical for known dangerous patterns
    for (const pattern of dangerousCommandPatterns) {
      if (pattern.test(command)) {
        analysis.triggers.push(`dangerous_command:${pattern.source}`);
        analysis.risk = "critical";
      }
    }

    // node_modules access gate
    if (command.includes("node_modules")) {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: "node_modules access blocked (no UI for confirmation)",
        };
      }
      const confirmed = await ctx.ui.confirm(
        "Node Modules Access",
        `Command accesses node_modules — a common injection vector.\n\n${command.substring(0, 300)}`,
      );
      if (!confirmed) {
        return { block: true, reason: "node_modules access denied by user" };
      }
      return undefined;
    }

    if (analysis.risk === "critical" || analysis.risk === "high") {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: `${analysis.risk.toUpperCase()} risk command blocked (no UI for confirmation)`,
        };
      }
      const confirmed = await ctx.ui.confirm(
        `${analysis.risk.toUpperCase()} Risk Command Detected`,
        `Suspicious patterns found:\n• ${analysis.triggers.join("\n• ")}\n\n${command.substring(0, 300)}`,
      );
      if (!confirmed) {
        return {
          block: true,
          reason: `${analysis.risk} risk command denied by user`,
        };
      }
    }

    return undefined;
  });

  // write / edit — content injection risk analysis before writing to disk
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "write" && event.toolName !== "edit")
      return undefined;

    const content =
      ((event.toolName === "write"
        ? event.input.content
        : event.input.path) as string) ?? "";
    const filePath = (event.input.path ?? "") as string;

    if (!content) return undefined;

    const source = detectSourceFromPath(filePath);
    const analysis = analyzeInjectionRisk(content, source);

    if (analysis.risk === "high" || analysis.risk === "critical") {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: `${analysis.risk.toUpperCase()} risk content blocked (no UI for confirmation)`,
        };
      }
      const confirmed = await ctx.ui.confirm(
        `${analysis.risk.toUpperCase()} Risk Content`,
        `Potentially malicious content detected before ${event.toolName}.\n• ${analysis.triggers.join("\n• ")}\n\nTo: ${filePath}`,
      );
      if (!confirmed) {
        return {
          block: true,
          reason: `${analysis.risk} risk content denied by user`,
        };
      }
    }

    return undefined;
  });
};

export default promptInjectionGuard;
