import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const promptInjectionGuard: ExtensionFactory = (pi) => {
  // Read configuration from environment (set by Go orchestrator)
  const enableProtection = process.env.AGENTLOOP_INJECTION_PROTECTION !== "false";
  const whitelistSources = (process.env.AGENTLOOP_WHITELIST_SOURCES || "")
    .split(",")
    .filter(Boolean);
  const blockedKeywords = (process.env.AGENTLOOP_BLOCKED_KEYWORDS || 
    "ignore previous instructions,forget everything above,act as if you are,pretend you are,roleplay as,API_KEY,SECRET,TOKEN,PASSWORD")
    .split(",")
    .filter(Boolean);
  const requireApproval = (process.env.AGENTLOOP_REQUIRE_APPROVAL || 
    "skills/*,node_modules/*,*/attachments/*,cloud:*,fetch:*,.git/*")
    .split(",")
    .filter(Boolean);
  const maxContentLength = parseInt(process.env.AGENTLOOP_MAX_CONTENT_LENGTH || "50000");

  if (!enableProtection) {
    return; // Protection disabled
  }

  // Injection risk detection patterns
  const sensitivePatterns = [
    /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b/g, // emails
    /\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b/g,                    // IP addresses
    /\b[A-Fa-f0-9]{32,}\b/g,                                 // hex keys
    /sk-[a-zA-Z0-9]{48}/g,                                   // OpenAI keys
    /xoxb-[0-9]+-[0-9]+-[0-9]+-[a-zA-Z0-9]+/g,              // Slack tokens
    /bearer\s+[a-zA-Z0-9._-]+/gi,                            // Bearer tokens
    /basic\s+[a-zA-Z0-9+/=]+/gi,                             // Basic auth
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
    /\<\|im_start\|\>/gi,
  ];

  // Detect injection source from file path
  function detectSourceFromPath(path: string): string {
    if (!path) return "unknown";
    
    const cleanPath = path.toLowerCase();
    if (cleanPath.includes("node_modules")) return "node_modules";
    if (cleanPath.includes("/skills/") || cleanPath.includes("\\skills\\")) return "skills";
    if (cleanPath.includes("/attachments/") || cleanPath.includes("\\attachments\\")) return "email_attachments";
    if (cleanPath.includes("/.git/") || cleanPath.includes("\\.git\\")) return "git_repo";
    if (path.startsWith("cloud:")) return "cloud_file";
    if (path.startsWith("fetch:")) return "fetch_response";
    return "user_input";
  }

  // Check if content contains injection patterns
  function analyzeInjectionRisk(content: string, source: string): {risk: string, triggers: string[]} {
    const triggers: string[] = [];
    let riskLevel = "low";

    if (!content) return {risk: riskLevel, triggers};

    // Check content length
    if (content.length > maxContentLength) {
      triggers.push("content_too_large");
      riskLevel = "high";
    }

    // Check for blocked keywords
    const lowerContent = content.toLowerCase();
    for (const keyword of blockedKeywords) {
      if (lowerContent.includes(keyword.toLowerCase())) {
        triggers.push(`blocked_keyword:${keyword}`);
        riskLevel = "medium";
      }
    }

    // Check for injection triggers
    for (const pattern of injectionTriggers) {
      if (pattern.test(content)) {
        triggers.push(`injection_pattern:${pattern.source}`);
        riskLevel = "high";
      }
    }

    // Check for sensitive patterns
    for (const pattern of sensitivePatterns) {
      if (pattern.test(content)) {
        triggers.push(`sensitive_data:${pattern.source}`);
        riskLevel = "high";
      }
    }

    // Source-based risk assessment
    switch (source) {
      case "skills":
        riskLevel = riskLevel === "low" ? "medium" : riskLevel;
        break;
      case "node_modules":
      case "email_attachments":
      case "cloud_file":
        riskLevel = riskLevel === "low" ? "high" : riskLevel;
        break;
      case "fetch_response":
      case "git_repo":
        riskLevel = riskLevel === "low" ? "medium" : riskLevel;
        break;
    }

    return {risk: riskLevel, triggers};
  }

  // Check if source requires approval
  function requiresApproval(path: string, source: string): boolean {
    if (!path) return false;

    for (const pattern of requireApproval) {
      if (pattern.includes("*")) {
        const regex = new RegExp(pattern.replace(/\*/g, ".*"));
        if (regex.test(path)) return true;
      } else if (path.includes(pattern)) {
        return true;
      }
    }

    return ["node_modules", "email_attachments", "cloud_file"].includes(source);
  }

  // Intercept read tool calls for content analysis
  pi.on("toolCall", async (event, ctx) => {
    if (event.tool.name !== "read") return { action: "continue" };

    const filePath = event.input?.path || event.input?.file_path || "";
    const source = detectSourceFromPath(filePath);

    // Check whitelist
    if (whitelistSources.length > 0 && filePath) {
      const isWhitelisted = whitelistSources.some(allowed => {
        const expanded = allowed.replace("~", process.env.HOME || "");
        return filePath.startsWith(expanded);
      });
      if (!isWhitelisted) {
        return {
          action: "request_permission",
          title: "Non-whitelisted File Access",
          message: `File "${filePath}" is outside whitelisted paths. Source: ${source}`,
          options: ["approve", "deny", "abort"]
        };
      }
    }

    // Check if requires approval
    if (requiresApproval(filePath, source)) {
      return {
        action: "request_permission", 
        title: "High-Risk File Access",
        message: `Reading file from potentially dangerous source: ${source}\nPath: ${filePath}`,
        options: ["approve", "deny", "abort"]
      };
    }

    return { action: "continue" };
  });

  // Intercept bash commands for injection analysis
  pi.on("toolCall", async (event, ctx) => {
    if (event.tool.name !== "bash") return { action: "continue" };

    const command = event.input?.command || "";
    if (!command) return { action: "continue" };

    const source = "bash_command";
    const analysis = analyzeInjectionRisk(command, source);

    // Check for dangerous patterns in commands
    const dangerousCommands = [
      /curl\s+.*\|\s*bash/gi,           // curl piped to bash
      /wget\s+.*\|\s*sh/gi,             // wget piped to shell
      /eval\s*\$\(/gi,                  // eval with command substitution  
      /exec\s*\$\(/gi,                  // exec with command substitution
      /rm\s+-rf\s+\/\w/gi,              // dangerous rm commands
      /chmod\s+777/gi,                  // overly permissive chmod
      /mkfs/gi,                         // filesystem creation
      /dd\s+if=/gi,                     // disk operations
    ];

    for (const pattern of dangerousCommands) {
      if (pattern.test(command)) {
        analysis.triggers.push(`dangerous_command:${pattern.source}`);
        analysis.risk = "critical";
      }
    }

    // Check for network requests from skills context
    if ((command.includes("curl") || command.includes("wget") || command.includes("fetch")) && 
        ctx.currentContext?.includes("skill")) {
      return {
        action: "request_permission",
        title: "Network Request from Skill",
        message: `Skill attempting network request:\n${command.substring(0, 200)}...`,
        options: ["approve", "deny", "abort"]
      };
    }

    // Check for node_modules access
    if (command.includes("node_modules")) {
      return {
        action: "request_permission", 
        title: "Node Modules Access",
        message: `Command accessing node_modules directory:\n${command.substring(0, 200)}...`,
        options: ["approve", "deny", "abort"]
      };
    }

    if (analysis.risk === "critical" || analysis.risk === "high") {
      return {
        action: "request_permission",
        title: `${analysis.risk.toUpperCase()} Risk Command`,
        message: `Command contains suspicious patterns:\n• ${analysis.triggers.join('\n• ')}\n\nCommand: ${command.substring(0, 200)}...`,
        options: ["approve", "deny", "abort"]
      };
    }

    return { action: "continue" };
  });

  // Intercept write/edit operations for content sanitization
  for (const toolName of ["write", "edit"]) {
    pi.on("toolCall", async (event, ctx) => {
      if (event.tool.name !== toolName) return { action: "continue" };

      const content = event.input?.content || event.input?.newText || "";
      const filePath = event.input?.path || event.input?.file_path || "";
      
      if (!content) return { action: "continue" };

      const source = detectSourceFromPath(filePath);
      const analysis = analyzeInjectionRisk(content, source);

      if (analysis.risk === "high" || analysis.risk === "critical") {
        return {
          action: "request_permission",
          title: `${analysis.risk.toUpperCase()} Risk Content`,
          message: `Attempting to ${toolName} potentially malicious content:\n• ${analysis.triggers.join('\n• ')}\n\nTo: ${filePath}`,
          options: ["approve", "deny", "abort"]
        };
      }

      return { action: "continue" };
    });
  }

  console.log("🛡️ Prompt Injection Guard loaded - Protection enabled");
};

export default promptInjectionGuard;