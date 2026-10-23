# Holybook for AI agents

This package lets AI assistants research the authoritative Bahá'í writings and translate texts
that quote them, using the official translations. It consists of

- the **holybook MCP server** at `https://holybook.app/mcp` (public, read-only, no sign-in), served
  by the Go server in this repository, with four tools:

  | Tool | Purpose |
  | --- | --- |
  | `search_writings` | Full-text search in one language, filterable by author and book; ranked paragraphs with citation and link |
  | `get_passage` | Read consecutive paragraphs of a book, e.g. the context of a search result |
  | `list_books` | Find works by author or title |
  | `find_authoritative_translation` | For each paragraph/quotation of a text being translated: is it authoritative, where is it from, and what is its authoritative translation in the target language |

- two **skills** that teach the agent how to use them:
  - [`bahai-writings-research`](skills/bahai-writings-research/SKILL.md) — research a question
    and answer with verbatim, cited quotations;
  - [`bahai-quote-translation`](skills/bahai-quote-translation/SKILL.md) — translate a text so
    that every quotation uses the authoritative translation.

The server also sends a summary of both skills as MCP *instructions* and offers them as MCP
*prompts* (`research_bahai_writings`, `translate_with_authoritative_quotes`), so clients that only
connect the MCP server still get the guidance.

## Installation

### Claude Code

```bash
claude plugin marketplace add holybook/go
claude plugin install holybook@holybook
```

This installs the MCP server and both skills. In a session, `/plugin` shows the plugin and `/mcp`
the server.

### Claude (claude.ai, Claude Desktop)

1. **Connector:** *Settings → Connectors → Add custom connector*, name `Holybook`, URL
   `https://holybook.app/mcp`. No authentication is needed.
2. **Skills:** run `scripts/package_agent_skills.sh` in this repository and upload
   `build/agent-packages/bahai-writings-research.zip` and
   `build/agent-packages/bahai-quote-translation.zip` under *Settings → Capabilities → Skills*.

On Team and Enterprise plans an owner adds the connector and can provision the skills for the
whole organization.

### Codex

```bash
codex plugin marketplace add holybook/go
```

Then install *Holybook* from the plugin list in Codex (or the plugins page of the ChatGPT desktop
app). Without plugins, the server alone can be added to
`~/.codex/config.toml`:

```toml
[mcp_servers.holybook]
url = "https://holybook.app/mcp"
```

and the skills copied to `~/.agents/skills/`.

### Gemini app (gemini.google.com)

The Gemini app connects MCP servers as *custom apps* but has no skills, so the guidance goes into a
Gem:

1. *Settings → Connected Apps → Custom apps → Add a custom app*, URL `https://holybook.app/mcp`.
   (Google currently limits custom apps to personal accounts in the US, in English, on the web.)
2. Run `scripts/package_agent_skills.sh`, create a new Gem (*Gems → New Gem*) named "Holybook"
   and paste `build/agent-packages/gemini-gem-instructions.md` as its instructions.

### Gemini CLI

The folder is also a Gemini CLI extension (MCP server, skills and `GEMINI.md` context):

```bash
git clone https://github.com/holybook/go.git holybook
gemini extensions install ./holybook/plugins/holybook
```

### Other MCP clients

Any client that supports remote MCP servers over Streamable HTTP can use
`https://holybook.app/mcp` directly. The skills follow the open
[Agent Skills](https://agentskills.io) format.

## Development

- Run the server locally (`make run`); the endpoint is then
  `http://localhost:8080/mcp`. Set `PUBLIC_BASE_URL` to change the links in tool results.
- Inspect it with the [MCP Inspector](https://github.com/modelcontextprotocol/inspector):
  `npx @modelcontextprotocol/inspector`, transport *Streamable HTTP*.
- To try the plugin against a local server, copy this folder, change the URL in `.mcp.json` and
  start `claude --plugin-dir <copy>`.
- The skills are embedded into the server binary (see `plugins/skills.go`), so editing a
  `SKILL.md` updates the plugin and the MCP prompts alike. Bump `version` in the
  three manifests (`.claude-plugin/`, `.codex-plugin/`, `gemini-extension.json`) on changes.
