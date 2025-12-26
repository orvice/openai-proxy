package workflows

import (
	"context"
	"os"

	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	oai "github.com/firebase/genkit/go/plugins/compat_oai"
)

func TestGenkit(t *testing.T) {
	if os.Getenv("OPENAI_PROVIDER") == "" || os.Getenv("OPENAI_KEY") == "" || os.Getenv("OPENAI_HOST") == "" {
		t.Skip("OPENAI_PROVIDER, OPENAI_KEY, OPENAI_HOST are not set")
	}
	config := &oai.OpenAICompatible{
		Provider: os.Getenv("OPENAI_PROVIDER"),
		APIKey:   os.Getenv("OPENAI_KEY"),
		BaseURL:  os.Getenv("OPENAI_HOST"),
	}
	g := genkit.Init(context.Background(), genkit.WithPlugins(config), genkit.WithDefaultModel("xiaomimimo/mimo-v2-flash"))
	resp, err := genkit.Generate(context.Background(), g, ai.WithPrompt("Hello, world!"))
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}
	t.Logf("Response: %v", resp)
}
