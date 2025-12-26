package workflows

import (
	"context"
	"net/http"
	"strings"

	"butterfly.orx.me/core/log"
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	oai "github.com/firebase/genkit/go/plugins/compat_oai"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/openai/openai-go/option"
	"github.com/orvice/openapi-proxy/internal/config"
)

var (
	g *genkit.Genkit

	travelPlanFlow *core.Flow[TravelPlanInput, *TravelPlan, struct{}]
)

func logMiddleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	logger := log.FromContext(req.Context()).With("component", "workflows")
	logger.Info("request", "method", req.Method, "url", req.URL.String())
	resp, err := next(req)
	if err != nil {
		logger.Error("request failed", "error", err)
		return nil, err
	}
	logger.Info("request completed", "status", resp.StatusCode)
	return resp, nil
}
func openaiPlugin() *oai.OpenAICompatible {
	vendor := config.Conf.GetWorkflowVender()

	baseURL := vendor.Host
	if !strings.Contains(baseURL, "v1") && !strings.Contains(baseURL, "v2") {
		baseURL = strings.TrimSuffix(baseURL, "/") + "/v1"
	}

	return &oai.OpenAICompatible{
		Provider: vendor.Name,
		APIKey:   vendor.Key,
		BaseURL:  baseURL,
		Opts: []option.RequestOption{
			option.WithMiddleware(logMiddleware),
		},
	}
}

func Init() error {
	ctx := context.Background()
	logger := log.FromContext(ctx).With("component", "workflows")

	vendor := config.Conf.GetWorkflowVender()

	plugins := []api.Plugin{}

	var models = "googleai/gemini-2.5-flash"

	if vendor.Name != "" {
		logger.Info("Initializing workflows with custom vendor",
			"vendor", vendor.Name,
			"host", vendor.Host,
			"default_model", vendor.DefaultModel)
		plugins = append(plugins, openaiPlugin())
		models = vendor.DefaultModel
	} else {
		logger.Info("Initializing workflows with Google AI",
			"default_model", models)
		plugins = append(plugins, &googlegenai.GoogleAI{
			APIKey: config.Conf.GoogleAIAPIKey,
		})
	}

	// Initialize Genkit with the plugins
	g = genkit.Init(ctx,
		genkit.WithPlugins(plugins...),
		genkit.WithDefaultModel(models),
	)

	logger.Info("Genkit initialized successfully")
	InitWorkflows()
	logger.Info("Workflows initialized successfully")
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
	Language        string `json:"language"`
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
			logger := log.FromContext(ctx)
			logger.Info("menuSuggestionFlow", "input", input)

			item, metadata, err := genkit.GenerateData[MenuItem](ctx, g,
				ai.WithPrompt("Invent a menu item for a %s themed restaurant.", input.Theme),
			)

			if err != nil {
				logger.Error("menuSuggestionFlow failed", "error", err)
				return nil, err
			}

			logger.Info("menuSuggestionFlow completed",
				"item", item,
				"usage", metadata.Usage)

			return item, err
		})

	// Travel planning workflow
	travelPlanFlow = genkit.DefineFlow(g, "travelPlanFlow",
		func(ctx context.Context, input TravelPlanInput) (*TravelPlan, error) {
			logger := log.FromContext(ctx)
			logger.Info("travelPlanFlow started", "input", input)

			lang := input.Language
			if lang == "" {
				lang = "Chinese"
			}

			prompt := `Create a detailed travel plan from %s to %s for %d days.
Please provide:
1. An overview of the trip
2. A day-by-day itinerary with activities, meals, and accommodation suggestions
3. Transportation recommendations
4. Budget estimates
5. Useful tips for travelers

Format the response as a structured travel plan.
Please respond in %s.`

			plan, metadata, err := genkit.GenerateData[TravelPlan](ctx, g,
				ai.WithPrompt(prompt, input.DepartureCity, input.DestinationCity, input.TravelDays, lang),
			)

			if err != nil {
				logger.Error("travelPlanFlow failed", "error", err)
				return nil, err
			}

			logger.Info("travelPlanFlow completed",
				"destination", plan.Destination,
				"duration", plan.Duration,
				"usage", metadata.Usage)

			return plan, err
		})

}
