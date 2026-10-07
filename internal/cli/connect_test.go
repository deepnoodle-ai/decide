package cli

import (
	"strings"
	"testing"
)

func TestConnectOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := connect("openai", ""); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("missing key: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_BASE_URL", "https://gateway.example.com/v1/")
	if _, err := connect("openai", ""); err != nil {
		t.Fatal(err)
	}
	if got := address("openai"); got != "https://gateway.example.com/v1" {
		t.Fatalf("cache address %q", got)
	}
	if defaultModels["openai"] != "gpt-6-luna" {
		t.Fatalf("default model %q", defaultModels["openai"])
	}
}
