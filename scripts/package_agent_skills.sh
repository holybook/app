#!/usr/bin/env bash
#
# Builds the files needed to use the holybook skills in chat apps that install skills by upload
# rather than through a plugin marketplace. The skills in plugins/holybook/skills are the single
# source; nothing here is checked in.
#
# Output (in build/agent-packages/):
#   <skill>.zip                  one per skill, for upload in Claude (claude.ai / Claude Desktop:
#                                Settings → Capabilities → Skills) or other apps that accept
#                                Agent Skills as zip files
#   gemini-gem-instructions.md   both skills as one text, to paste into the instructions of a
#                                Gemini Gem (the Gemini app has no skill support)
#
# Usage: scripts/package_agent_skills.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SKILLS="$ROOT/plugins/holybook/skills"
OUT="$ROOT/build/agent-packages"

rm -rf "$OUT"
mkdir -p "$OUT"

for dir in "$SKILLS"/*/; do
  name="$(basename "$dir")"
  # The archive must contain the skill folder itself, with SKILL.md inside it.
  (cd "$SKILLS" && zip -qr "$OUT/$name.zip" "$name" -x '*.DS_Store')
  echo "Built $OUT/$name.zip"
done

# Strips the YAML front matter (the first block delimited by --- lines) from a SKILL.md.
skill_body() {
  awk 'NR == 1 && /^---$/ { in_front = 1; next } in_front && /^---$/ { in_front = 0; next } !in_front' "$1"
}

{
  cat <<'EOF'
You help people study the Bahá'í writings and translate texts that quote them. Use the connected
Holybook app (MCP server https://holybook.app/mcp) for every answer about the writings and for
every quotation in a translation. Follow the two workflows below.

EOF
  for dir in "$SKILLS"/*/; do
    skill_body "$dir/SKILL.md"
    printf '\n'
  done
} > "$OUT/gemini-gem-instructions.md"
echo "Built $OUT/gemini-gem-instructions.md"
