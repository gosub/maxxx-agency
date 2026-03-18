package coach

import (
	"fmt"
	"strings"
)

func BuildSystemPrompt(compendium, language, tone string, phase int, goals, rejections []string) string {
	var sb strings.Builder

	sb.WriteString("You are Maxxx Agency, a personal agency coach. You are concise, direct, and push when needed. Never preachy.\n\n")

	sb.WriteString(fmt.Sprintf("Always respond in %s.\n\n", langName(language)))

	switch tone {
	case "direct":
		sb.WriteString("Be direct and efficient. Short responses. No fluff. Get to the point.\n\n")
	case "drill-sergeant":
		sb.WriteString("Be intense and demanding. Push hard. No excuses. Tough love.\n\n")
	default:
		sb.WriteString("Be warm and encouraging. Celebrate small wins. Gentle nudges.\n\n")
	}

	sb.WriteString("--- AGENCY FRAMEWORK REFERENCE ---\n\n")
	sb.WriteString(compendium)
	sb.WriteString("\n\n--- END REFERENCE ---\n\n")

	sb.WriteString(fmt.Sprintf("Current user state:\n- Phase: %d\n", phase))
	if len(goals) > 0 {
		sb.WriteString(fmt.Sprintf("- Active goals: %s\n", strings.Join(goals, ", ")))
	} else {
		sb.WriteString("- Active goals: none\n")
	}
	sb.WriteString(fmt.Sprintf("- Rejections logged: %d\n", len(rejections)))
	sb.WriteString("\n")

	sb.WriteString("Behavioral rules:\n")
	sb.WriteString("- Ask one good question at a time\n")
	sb.WriteString("- Don't be nosy — brief check-ins, go deeper only if user engages\n")
	sb.WriteString("- Push gently when detecting avoidance or procrastination\n")
	sb.WriteString("- Celebrate rejections and small wins\n")
	sb.WriteString("- Suggest next phase when current tasks are done\n")

	return sb.String()
}

func langName(code string) string {
	switch code {
	case "it":
		return "Italian"
	case "en":
		return "English"
	default:
		return "English"
	}
}
