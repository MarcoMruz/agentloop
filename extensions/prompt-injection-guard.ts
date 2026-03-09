import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

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
  // Detection patterns
  // ---------------------------------------------------------------------------

  const sensitivePatterns = [
    /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b/g, // emails
    /\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b/g,                    // IP addresses
    /sk-[a-zA-Z0-9]{48}/g,                                    // OpenAI keys  (must precede hex)
    /xoxb-[0-9]+-[0-9]+-[0-9]+-[a-zA-Z0-9]+/g,               // Slack tokens (must precede hex)
    /bearer\s+[a-zA-Z0-9._-]+/gi,                             // Bearer tokens
    /basic\s+[a-zA-Z0-9+/=]+/gi,                              // Basic auth
    /\b[A-Fa-f0-9]{32,}\b/g,                                  // hex keys (broad catch-all, must be last)
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
    { pattern: /curl\s+.*\|\s*bash/gi,  label: "curl piped to bash (remote code execution)" },
    { pattern: /wget\s+.*\|\s*sh/gi,    label: "wget piped to shell (remote code execution)" },
    { pattern: /eval\s*\$\(/gi,         label: "eval with command substitution" },
    { pattern: /exec\s*\$\(/gi,         label: "exec with command substitution" },
    { pattern: /rm\s+-rf\s+\/\w/gi,     label: "destructive rm -rf on system path" },
    { pattern: /chmod\s+777/gi,         label: "overly permissive chmod 777" },
    { pattern: /mkfs/gi,                label: "filesystem creation (mkfs)" },
    { pattern: /dd\s+if=/gi,            label: "raw disk operation (dd)" },
  ];

  // ---------------------------------------------------------------------------
  // Temp file writer — creates /tmp/agentloop-hitl-<timestamp>.md
  // ---------------------------------------------------------------------------

  function writeTempMd(sections: string[]): string {
    const name = `agentloop-hitl-${Date.now()}-${Math.floor(Math.random() * 1e6)}.md`;
    const filePath = path.join(os.tmpdir(), name);
    fs.writeFileSync(filePath, sections.join("\n\n"), "utf8");
    return filePath;
  }

  /** Human-readable byte size. */
  function sizeOf(text: string): string {
    const bytes = Buffer.byteLength(text, "utf8");
    return bytes >= 1024 ? `${(bytes / 1024).toFixed(1)} KB` : `${bytes} B`;
  }

  // ---------------------------------------------------------------------------
  // Analysis helpers
  // ---------------------------------------------------------------------------

  function detectSourceFromPath(filePath: string): string {
    if (!filePath) return "unknown";
    const lower = filePath.toLowerCase();
    if (lower.includes("node_modules"))                                          return "node_modules";
    if (lower.includes("/skills/") || lower.includes("\\skills\\"))              return "skills";
    if (lower.includes("/attachments/") || lower.includes("\\attachments\\"))    return "email_attachments";
    if (lower.includes("/.git/") || lower.includes("\\.git\\"))                  return "git_repo";
    if (filePath.startsWith("cloud:"))                                            return "cloud_file";
    if (filePath.startsWith("fetch:"))                                            return "fetch_response";
    return "user_input";
  }

  function sourceRiskLabel(source: string): string {
    switch (source) {
      case "node_modules":      return "⚠️ node_modules (common injection vector)";
      case "email_attachments": return "🚨 email attachment (high-risk external content)";
      case "cloud_file":        return "🚨 cloud storage file (external, unverified)";
      case "fetch_response":    return "⚠️ network fetch response (external content)";
      case "git_repo":          return "⚠️ git repository file (.git internals)";
      case "skills":            return "⚠️ skills directory (agent instruction file)";
      default:                  return `ℹ️ ${source}`;
    }
  }

  function analyzeInjectionRisk(
    content: string,
    source: string,
  ): { risk: string; triggers: string[] } {
    const triggers: string[] = [];
    let riskLevel = "low";

    if (!content) return { risk: riskLevel, triggers };

    if (content.length > maxContentLength) {
      triggers.push(`content_too_large (${content.length} chars > ${maxContentLength} limit)`);
      riskLevel = "high";
    }

    const lower = content.toLowerCase();
    blockedKeywords.forEach((kw) => {
      if (lower.includes(kw.toLowerCase())) {
        triggers.push(`blocked keyword: "${kw}"`);
        if (riskLevel === "low") riskLevel = "medium";
      }
    });

    injectionTriggers.forEach((re) => {
      if (re.test(content)) {
        triggers.push(`injection pattern: \`/${re.source}/\``);
        riskLevel = "high";
      }
    });

    sensitivePatterns.forEach((re) => {
      if (re.test(content)) {
        triggers.push(`sensitive data: \`/${re.source}/\``);
        riskLevel = "high";
      }
    });

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

  function requiresApproval(filePath: string, source: string): boolean {
    if (!filePath) return false;
    const matched = requireApprovalPatterns.find((pattern) =>
      pattern.includes("*")
        ? new RegExp(pattern.replace(/\*/g, ".*")).test(filePath)
        : filePath.includes(pattern),
    );
    if (matched) return true;
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

    // Whitelist gate
    if (whitelistSources.length > 0 && filePath) {
      const isWhitelisted = whitelistSources.some((allowed) => {
        const expanded = allowed.replace("~", process.env.HOME ?? "");
        return filePath.startsWith(expanded);
      });

      if (!isWhitelisted) {
        if (!ctx.hasUI) {
          return { block: true, reason: `File "${filePath}" is outside whitelisted paths` };
        }

        const detailsFile = writeTempMd([
          "# File Read Request — Outside Whitelisted Paths",
          "## Action if Approved\nRead (view) the full contents of this file.",
          `## File\n\`\`\`\n${filePath}\n\`\`\``,
          `## Detected Source\n${sourceRiskLabel(source)}`,
          "## Why This Prompt?\nThis file is not under any of the configured allowed paths.",
          `## Whitelisted Paths\n${whitelistSources.map((p) => `- \`${p}\``).join("\n") || "_none configured_"}`,
        ]);

        const confirmed = await ctx.ui.confirm(
          "📂 File Read — Outside Whitelisted Paths",
          `Action if approved: read the full contents of this file.\n\n📄 Full details: ${detailsFile}`,
        );
        if (!confirmed) return { block: true, reason: "File access denied by user" };
      }
    }

    // High-risk source gate
    if (requiresApproval(filePath, source)) {
      if (!ctx.hasUI) {
        return { block: true, reason: `High-risk file access blocked (source: ${source})` };
      }

      const detailsFile = writeTempMd([
        "# File Read Request — High-Risk Source",
        "## Action if Approved\nRead (view) the full contents of this file.",
        `## File\n\`\`\`\n${filePath}\n\`\`\``,
        `## Detected Source\n${sourceRiskLabel(source)}`,
        "## Why This Prompt?\nThis file's location is classified as a high-risk injection source.\nMalicious content in this file could manipulate the agent's behaviour.",
        `## Approval Patterns That Matched\n${requireApprovalPatterns.map((p) => `- \`${p}\``).join("\n")}`,
      ]);

      const confirmed = await ctx.ui.confirm(
        "📂 File Read — High-Risk Source",
        `Action if approved: read the full contents of this file.\n\n📄 Full details: ${detailsFile}`,
      );
      if (!confirmed) return { block: true, reason: "High-risk file access denied by user" };
    }

    return undefined;
  });

  // bash — dangerous command patterns + injection risk analysis
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;

    const command = (event.input.command ?? "") as string;
    if (!command) return undefined;

    const analysis = analyzeInjectionRisk(command, "bash_command");
    const matchedDangerous = dangerousCommandPatterns.find(({ pattern }) =>
      pattern.test(command),
    );
    if (matchedDangerous) {
      analysis.triggers.push(`dangerous pattern: ${matchedDangerous.label}`);
      analysis.risk = "critical";
    }

    // node_modules access gate
    if (command.includes("node_modules")) {
      if (!ctx.hasUI) {
        return { block: true, reason: "node_modules access blocked (no UI for confirmation)" };
      }

      const detailsFile = writeTempMd([
        "# Shell Command — node_modules Access",
        "## Action if Approved\nExecute the following command in the shell.",
        `## Command\n\`\`\`bash\n${command}\n\`\`\``,
        "## Why This Prompt?\n`node_modules` is a common prompt-injection vector.\nPackages installed here may contain malicious scripts that alter agent behaviour.",
      ]);

      const confirmed = await ctx.ui.confirm(
        "🔍 Shell Command — node_modules Access",
        `Action if approved: execute the shell command.\n\n📄 Full details: ${detailsFile}`,
      );
      if (!confirmed) return { block: true, reason: "node_modules access denied by user" };
      return undefined;
    }

    if (analysis.risk === "critical" || analysis.risk === "high") {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: `${analysis.risk.toUpperCase()} risk command blocked (no UI for confirmation)`,
        };
      }

      const riskEmoji = analysis.risk === "critical" ? "🚨" : "⚠️";
      const detailsFile = writeTempMd([
        `# Shell Command — ${analysis.risk.toUpperCase()} Risk`,
        "## Action if Approved\nExecute the following command in the shell.",
        `## Command\n\`\`\`bash\n${command}\n\`\`\``,
        `## Triggered Checks (${analysis.triggers.length})\n${analysis.triggers.map((t) => `- ${t}`).join("\n")}`,
      ]);

      const confirmed = await ctx.ui.confirm(
        `${riskEmoji} Shell Command — ${analysis.risk.toUpperCase()} Risk`,
        `Action if approved: execute the shell command.\n\n📄 Full details: ${detailsFile}`,
      );
      if (!confirmed) {
        return { block: true, reason: `${analysis.risk} risk command denied by user` };
      }
    }

    return undefined;
  });

  // write — content injection risk analysis before writing to disk
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "write") return undefined;

    const filePath = (event.input.path ?? "") as string;
    const content = (event.input.content ?? "") as string;
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

      const riskEmoji = analysis.risk === "critical" ? "🚨" : "⚠️";
      const size = sizeOf(content);
      const detailsFile = writeTempMd([
        `# File Write — ${analysis.risk.toUpperCase()} Risk Content`,
        `## Action if Approved\nCreate or overwrite \`${filePath}\` with ${size} of new content.`,
        `## Destination\n\`\`\`\n${filePath}\n\`\`\``,
        `## Content (${size})\n\`\`\`\n${content}\n\`\`\``,
        `## Triggered Checks (${analysis.triggers.length})\n${analysis.triggers.map((t) => `- ${t}`).join("\n")}`,
      ]);

      const confirmed = await ctx.ui.confirm(
        `${riskEmoji} File Write — ${analysis.risk.toUpperCase()} Risk`,
        `Action if approved: overwrite \`${filePath}\` with ${size} of new content.\n\n📄 Full details: ${detailsFile}`,
      );
      if (!confirmed) {
        return { block: true, reason: `${analysis.risk} risk content denied by user` };
      }
    }

    return undefined;
  });

  // edit — content injection risk analysis on replacement text
  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "edit") return undefined;

    const filePath = (event.input.path ?? "") as string;
    const oldText = (event.input.oldText ?? "") as string;
    const newText = (event.input.newText ?? "") as string;
    if (!newText) return undefined;

    const source = detectSourceFromPath(filePath);
    const analysis = analyzeInjectionRisk(newText, source);

    if (analysis.risk === "high" || analysis.risk === "critical") {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: `${analysis.risk.toUpperCase()} risk content blocked (no UI for confirmation)`,
        };
      }

      const riskEmoji = analysis.risk === "critical" ? "🚨" : "⚠️";
      const insertedSize = sizeOf(newText);
      const detailsFile = writeTempMd([
        `# File Edit — ${analysis.risk.toUpperCase()} Risk Content`,
        `## Action if Approved\nReplace a section in \`${filePath}\`.`,
        `## Target File\n\`\`\`\n${filePath}\n\`\`\``,
        `## Text Being Removed\n\`\`\`\n${oldText}\n\`\`\``,
        `## Text Being Inserted (${insertedSize})\n\`\`\`\n${newText}\n\`\`\``,
        `## Triggered Checks (${analysis.triggers.length})\n${analysis.triggers.map((t) => `- ${t}`).join("\n")}`,
      ]);

      const confirmed = await ctx.ui.confirm(
        `${riskEmoji} File Edit — ${analysis.risk.toUpperCase()} Risk`,
        `Action if approved: replace a section in \`${filePath}\` with ${insertedSize} of new text.\n\n📄 Full details: ${detailsFile}`,
      );
      if (!confirmed) {
        return { block: true, reason: `${analysis.risk} risk content denied by user` };
      }
    }

    return undefined;
  });
};

export default promptInjectionGuard;
