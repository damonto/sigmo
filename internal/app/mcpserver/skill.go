package mcpserver

import sigmoskills "github.com/damonto/sigmo/skills"

func SkillArchive() ([]byte, error) {
	return sigmoskills.SigmoControlArchive()
}
