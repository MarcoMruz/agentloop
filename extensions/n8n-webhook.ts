import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const n8nWebhook: ExtensionFactory = (pi) => {
  // Parse webhook config from env (set by Go orchestrator)
  // Format: AGENTLOOP_N8N_WEBHOOKS=name1:url1:header1:envvar1,name2:url2:header2:envvar2
  const webhooksRaw = process.env.AGENTLOOP_N8N_WEBHOOKS || "";
  const webhooks: Record<string, { url: string; authHeader: string; secretEnvVar: string }> = {};

  for (const entry of webhooksRaw.split(",").filter(Boolean)) {
    const [name, url, header, envVar] = entry.split(":");
    if (name && url) {
      webhooks[name] = { url, authHeader: header || "", secretEnvVar: envVar || "" };
    }
  }

  if (Object.keys(webhooks).length === 0) return;

  pi.addTool({
    name: "n8n_webhook",
    description: `Trigger a named n8n webhook. Available: ${Object.keys(webhooks).join(", ")}`,
    parameters: {
      type: "object",
      properties: {
        webhook_name: { type: "string", description: "Webhook name", enum: Object.keys(webhooks) },
        payload: { type: "string", description: "JSON payload string" },
      },
      required: ["webhook_name"],
    },
    async execute(input) {
      const wh = webhooks[input.webhook_name];
      if (!wh) return { error: `Webhook "${input.webhook_name}" not configured` };

      const headers: Record<string, string> = { "Content-Type": "application/json" };
      if (wh.authHeader && wh.secretEnvVar) {
        const secret = process.env[wh.secretEnvVar];
        if (secret) headers[wh.authHeader] = secret;
      }

      let body = {};
      if (input.payload) {
        try {
          body = JSON.parse(input.payload);
        } catch {
          return { error: "Invalid JSON payload" };
        }
      }

      const resp = await fetch(wh.url, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
      });

      if (!resp.ok) return { error: `Webhook error ${resp.status}: ${await resp.text()}` };
      return { output: await resp.text() };
    },
  });
};

export default n8nWebhook;
