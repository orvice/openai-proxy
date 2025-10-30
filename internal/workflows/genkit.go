package workflows

import (
	"context"

	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/orvice/openapi-proxy/internal/config"
)

var (
	g *genkit.Genkit
)

func Init() {
	ctx := context.Background()
	// Initialize Genkit with the Google AI plugin
	g = genkit.Init(ctx,
		genkit.WithPlugins(&googlegenai.GoogleAI{
			APIKey: config.Conf.GoogleAIAPIKey,
		}),
		genkit.WithDefaultModel("googleai/gemini-2.5-flash"),
	)
}

func Genkit() *genkit.Genkit {
	return g
}
