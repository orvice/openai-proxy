package workflows

import (
	"context"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/orvice/openapi-proxy/internal/config"
)

var (
	g *genkit.Genkit
)

func Init() error {
	ctx := context.Background()
	// Initialize Genkit with the Google AI plugin
	g = genkit.Init(ctx,
		genkit.WithPlugins(&googlegenai.GoogleAI{
			APIKey: config.Conf.GoogleAIAPIKey,
		}),
		genkit.WithDefaultModel("googleai/gemini-2.5-flash"),
	)
	InitWorkflows()
	return nil
}

func Genkit() *genkit.Genkit {
	return g
}

// menu workflow
type MenuSuggestionInput struct {
	Theme string `json:"theme"`
}

type MenuItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func InitWorkflows() {
	menuSuggestionFlow := genkit.DefineFlow(g, "menuSuggestionFlow",
		func(ctx context.Context, input MenuSuggestionInput) (*MenuItem, error) {
			item, _, err := genkit.GenerateData[MenuItem](ctx, g,
				ai.WithPrompt("Invent a menu item for a %s themed restaurant.", input.Theme),
			)
			return item, err
		})
	genkit.RegisterAction(g, menuSuggestionFlow)
}
