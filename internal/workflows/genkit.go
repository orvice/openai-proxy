package workflows

import (
	"context"

	"butterfly.orx.me/core/log"
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	oai "github.com/firebase/genkit/go/plugins/compat_oai"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/orvice/openapi-proxy/internal/config"
)

var (
	g *genkit.Genkit

	travelPlanFlow *core.Flow[TravelPlanInput, *TravelPlan, struct{}]
)

func openaiPlugin() *oai.OpenAICompatible {
	vendor := config.Conf.GetWorkflowVender()
	return &oai.OpenAICompatible{
		Provider: vendor.Name,
		APIKey:   vendor.Key,
		BaseURL:  vendor.Host,
	}
}

func Init() error {
	ctx := context.Background()

	vendor := config.Conf.GetWorkflowVender()

	plugins := []api.Plugin{}

	var models = "googleai/gemini-2.5-flash"

	if vendor.Name != "" {
		plugins = append(plugins, openaiPlugin())
		models = vendor.DefaultModel
	} else {
		plugins = append(plugins, &googlegenai.GoogleAI{
			APIKey: config.Conf.GoogleAIAPIKey,
		})
	}

	// Initialize Genkit with the Google AI plugin
	g = genkit.Init(ctx,
		genkit.WithPlugins(&googlegenai.GoogleAI{
			APIKey: config.Conf.GoogleAIAPIKey,
		}),
		genkit.WithDefaultModel(models),
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

// travel planning workflow
type TravelPlanInput struct {
	DepartureCity   string `json:"departure_city"`
	DestinationCity string `json:"destination_city"`
	TravelDays      int    `json:"travel_days"`
}

type DayItinerary struct {
	Day           int      `json:"day"`
	Activities    []string `json:"activities"`
	Meals         []string `json:"meals"`
	Accommodation string   `json:"accommodation,omitempty"`
}

type TravelPlan struct {
	Destination    string         `json:"destination"`
	Duration       int            `json:"duration"`
	Overview       string         `json:"overview"`
	DailyPlan      []DayItinerary `json:"daily_plan"`
	Transportation string         `json:"transportation"`
	Budget         string         `json:"budget"`
	Tips           []string       `json:"tips"`
}

func InitWorkflows() {
	genkit.DefineFlow(g, "menuSuggestionFlow",
		func(ctx context.Context, input MenuSuggestionInput) (*MenuItem, error) {
			item, _, err := genkit.GenerateData[MenuItem](ctx, g,
				ai.WithPrompt("Invent a menu item for a %s themed restaurant.", input.Theme),
			)
			return item, err
		})

	// Travel planning workflow
	travelPlanFlow = genkit.DefineFlow(g, "travelPlanFlow",
		func(ctx context.Context, input TravelPlanInput) (*TravelPlan, error) {
			logger := log.FromContext(ctx)
			logger.Info("travelPlanFlow", "input", input)
			prompt := `Create a detailed travel plan from %s to %s for %d days.
Please provide:
1. An overview of the trip
2. A day-by-day itinerary with activities, meals, and accommodation suggestions
3. Transportation recommendations
4. Budget estimates
5. Useful tips for travelers

Format the response as a structured travel plan.`

			plan, _, err := genkit.GenerateData[TravelPlan](ctx, g,
				ai.WithPrompt(prompt, input.DepartureCity, input.DestinationCity, input.TravelDays),
			)
			return plan, err
		})

}
