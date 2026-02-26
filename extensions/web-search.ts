import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const webSearch: ExtensionFactory = (pi) => {
  pi.addTool({
    name: "web_search",
    description: "Search the web using Brave Search. Returns titles, URLs, and snippets.",
    parameters: {
      type: "object",
      properties: {
        query: { type: "string", description: "Search query" },
        max_results: { type: "number", description: "Max results (default 5, max 10)" },
      },
      required: ["query"],
    },
    async execute(input) {
      const apiKey = process.env.BRAVE_SEARCH_API_KEY;
      if (!apiKey) return { error: "BRAVE_SEARCH_API_KEY not set" };

      const maxResults = Math.min(input.max_results || 5, 10);
      const url = `https://api.search.brave.com/res/v1/web/search?q=${encodeURIComponent(input.query)}&count=${maxResults}`;

      const resp = await fetch(url, {
        headers: {
          Accept: "application/json",
          "X-Subscription-Token": apiKey,
        },
      });

      if (!resp.ok) return { error: `Search error: ${resp.status}` };
      const data = await resp.json();
      const results = (data.web?.results || [])
        .map((r: any, i: number) => `${i + 1}. ${r.title}\n   ${r.url}\n   ${r.description}`)
        .join("\n\n");

      return { output: results || "No results found." };
    },
  });
};

export default webSearch;
