package tui

import "strings"

// ProviderBadge returns a standardized 3-character uppercase badge for display in session rows.
// Badges are fixed at exactly 3 columns width.
func ProviderBadge(providerID string) string {
	switch strings.ToLower(providerID) {
	case "antigravity", "agy":
		return "AGY"
	case "kiro", "kiro-cli":
		return "KIR"
	case "claude", "claude-code":
		return "CLD"
	case "codex":
		return "CDX"
	case "cursor":
		return "CRS"
	case "deepseek", "dsk":
		return "DSK"
	case "glm", "zhipu":
		return "GLM"
	case "aider":
		return "AID"
	case "opencode":
		return "OPC"
	default:
		cleaned := strings.TrimSpace(providerID)
		if len(cleaned) >= 3 {
			return strings.ToUpper(cleaned[:3])
		}
		if len(cleaned) == 2 {
			return strings.ToUpper(cleaned) + " "
		}
		if len(cleaned) == 1 {
			return strings.ToUpper(cleaned) + "  "
		}
		return "   "
	}
}
