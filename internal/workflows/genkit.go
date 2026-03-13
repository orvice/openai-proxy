package workflows

import (
	"context"
	"errors"
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
	"github.com/orvice/aiproxy/internal/config"
)

// Translation workflow errors
var (
	ErrEmptyInput          = errors.New("input text cannot be empty")
	ErrUnsupportedLanguage = errors.New("unsupported language")
)

var (
	g *genkit.Genkit

	travelPlanFlow  *core.Flow[TravelPlanInput, *TravelPlan, struct{}]
	translationFlow *core.Flow[TranslationInput, *TranslationOutput, struct{}]
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
	Language        string `json:"language,omitempty"`
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

// translation workflow
type TranslationInput struct {
	Text           string `json:"text"`            // 要翻译的文本
	SourceLanguage string `json:"source_language"` // 源语言（必填，使用 "auto" 表示自动检测）
	TargetLanguage string `json:"target_language"` // 目标语言（必填）
}

type TranslationOutput struct {
	TranslatedText string  `json:"translated_text"`      // 翻译后的文本
	SourceLanguage string  `json:"source_language"`      // 检测到的源语言
	TargetLanguage string  `json:"target_language"`      // 目标语言
	Confidence     float64 `json:"confidence,omitempty"` // 翻译置信度（可选）
}

// SupportedLanguages 包含所有支持的语言（包括 auto 用于源语言自动检测）
var SupportedLanguages = map[string]string{
	"auto": "Auto-detect", // 仅用于 source_language
	"zh":   "Chinese",
	"en":   "English",
	"ja":   "Japanese",
	"ko":   "Korean",
	"fr":   "French",
	"de":   "German",
	"es":   "Spanish",
}

// ValidTargetLanguages 不包含 "auto"，用于验证目标语言
var ValidTargetLanguages = map[string]string{
	"zh": "Chinese",
	"en": "English",
	"ja": "Japanese",
	"ko": "Korean",
	"fr": "French",
	"de": "German",
	"es": "Spanish",
}

// ValidateTranslationInput validates the translation input
// Returns nil if valid, otherwise returns an appropriate error
func ValidateTranslationInput(input TranslationInput) error {
	// Validate text is non-empty (after trimming whitespace)
	if strings.TrimSpace(input.Text) == "" {
		return ErrEmptyInput
	}

	// Validate source_language is in SupportedLanguages (includes "auto")
	if _, ok := SupportedLanguages[input.SourceLanguage]; !ok {
		return ErrUnsupportedLanguage
	}

	// Validate target_language is in ValidTargetLanguages (excludes "auto")
	if _, ok := ValidTargetLanguages[input.TargetLanguage]; !ok {
		return ErrUnsupportedLanguage
	}

	return nil
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

	// Translation workflow
	translationFlow = genkit.DefineFlow(g, "translationFlow",
		func(ctx context.Context, input TranslationInput) (*TranslationOutput, error) {
			logger := log.FromContext(ctx)
			logger.Info("translationFlow started",
				"source_language", input.SourceLanguage,
				"target_language", input.TargetLanguage,
				"text_length", len(input.Text))

			// Validate input
			if err := ValidateTranslationInput(input); err != nil {
				logger.Error("translationFlow validation failed", "error", err)
				return nil, err
			}

			// Build source language description for prompt
			sourceLangDesc := "auto-detect the source language"
			if input.SourceLanguage != "auto" {
				if langName, ok := SupportedLanguages[input.SourceLanguage]; ok {
					sourceLangDesc = langName
				}
			}

			// Get target language name
			targetLangName := ValidTargetLanguages[input.TargetLanguage]

			prompt := `Translate the following text from %s to %s.
If source language is "auto-detect the source language", please detect the source language automatically.

Text to translate:
%s

Please respond with a JSON object containing:
- translated_text: the translated text
- source_language: the detected/confirmed source language code (zh, en, ja, ko, fr, de, es)
- target_language: the target language code
- confidence: a number between 0 and 1 indicating translation confidence`

			output, metadata, err := genkit.GenerateData[TranslationOutput](ctx, g,
				ai.WithPrompt(prompt, sourceLangDesc, targetLangName, input.Text),
			)

			if err != nil {
				logger.Error("translationFlow failed", "error", err)
				return nil, err
			}

			// Ensure target_language matches the requested one
			output.TargetLanguage = input.TargetLanguage

			logger.Info("translationFlow completed",
				"source_language", output.SourceLanguage,
				"target_language", output.TargetLanguage,
				"usage", metadata.Usage)

			return output, nil
		})
}
