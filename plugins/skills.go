// Package plugins embeds the agent skills of the holybook plugin. The skills
// in holybook/skills are the single source of the guidance: the plugin ships
// them to agents, and the MCP server serves them as prompts.
package plugins

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed holybook/skills/*/SKILL.md
var skills embed.FS

var frontMatter = regexp.MustCompile(`(?s)\A---\n.*?\n---\n`)

// SkillBody returns the instructions of a skill without its YAML front
// matter.
func SkillBody(name string) (string, error) {
	data, err := fs.ReadFile(skills, "holybook/skills/"+name+"/SKILL.md")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(frontMatter.ReplaceAllString(string(data), "")), nil
}
