package workbench

import (
	"encoding/json"
	"os"
	"strings"
)

// redactCredentials covers native text and JSON string representations, including
// nested evidence payloads. Only configured provider credentials are considered.
func redactCredentials(text string) string {
	for _, name := range []string{"TYPESAFE_API_KEY", "CLOUDFLARE_AUTH_TOKEN"} {
		key := os.Getenv(name)
		if key == "" {
			continue
		}
		text = strings.ReplaceAll(text, key, "[REDACTED]")
		encoded := key
		for range 3 {
			b, _ := json.Marshal(encoded)
			encoded = string(b[1 : len(b)-1])
			text = strings.ReplaceAll(text, encoded, "[REDACTED]")
		}
	}
	return text
}

func containsCredential(text string) bool { return redactCredentials(text) != text }
