// extensions/vault-skills.ts
// Exposes AgentLoop vault skills to pi's native skill discovery.
// Uses resources_discover to register vault/skills/* directories so pi
// lists them in <available_skills> instead of (or alongside) its own skills.

import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export default function vaultSkillsExtension(pi: ExtensionAPI) {
  pi.on("resources_discover", () => {
    const vaultPath = process.env.AGENTLOOP_VAULT_PATH;
    if (!vaultPath) return {};

    const skillsDir = join(vaultPath, "skills");
    if (!existsSync(skillsDir)) return {};

    const skillPaths: string[] = [];
    try {
      const entries = readdirSync(skillsDir, { withFileTypes: true });
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        const skillMd = join(skillsDir, entry.name, "SKILL.md");
        if (existsSync(skillMd)) {
          skillPaths.push(skillMd);
        }
      }
    } catch {
      return {};
    }

    return skillPaths.length > 0 ? { skillPaths } : {};
  });
}
