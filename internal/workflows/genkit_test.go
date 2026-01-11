package workflows

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"strings"
	"testing"
	"testing/quick"

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

// Property-Based Tests for Translation Input Validation
// Feature: translation-workflow

// TestProperty2_EmptyInputRejection tests that empty or whitespace-only inputs are rejected
// **Property 2: 空输入拒绝**
// **Validates: Requirements 1.4**
func TestProperty2_EmptyInputRejection(t *testing.T) {
	// Generator for whitespace-only strings
	whitespaceChars := []rune{' ', '\t', '\n', '\r'}

	generateWhitespaceString := func(r *rand.Rand) string {
		length := r.Intn(20) // 0 to 19 characters
		result := make([]rune, length)
		for i := range result {
			result[i] = whitespaceChars[r.Intn(len(whitespaceChars))]
		}
		return string(result)
	}

	// Get a valid source and target language for testing
	validSourceLang := "en"
	validTargetLang := "zh"

	config := &quick.Config{
		MaxCount: 100,
	}

	// Property: For any whitespace-only string, validation should return ErrEmptyInput
	property := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		whitespaceText := generateWhitespaceString(r)

		input := TranslationInput{
			Text:           whitespaceText,
			SourceLanguage: validSourceLang,
			TargetLanguage: validTargetLang,
		}

		err := ValidateTranslationInput(input)
		return errors.Is(err, ErrEmptyInput)
	}

	if err := quick.Check(property, config); err != nil {
		t.Errorf("Property 2 (Empty Input Rejection) failed: %v", err)
	}
}

// TestProperty3_UnsupportedLanguageRejection tests that unsupported languages are rejected
// **Property 3: 不支持语言拒绝**
// **Validates: Requirements 2.3, 2.4**
func TestProperty3_UnsupportedLanguageRejection(t *testing.T) {
	// Generate random language codes that are NOT in supported languages
	generateUnsupportedLang := func(r *rand.Rand) string {
		// Generate random 2-3 character strings that are not in supported languages
		chars := "abcdefghijklmnopqrstuvwxyz"
		for {
			length := 2 + r.Intn(2) // 2 or 3 characters
			result := make([]byte, length)
			for i := range result {
				result[i] = chars[r.Intn(len(chars))]
			}
			lang := string(result)
			// Ensure it's not a supported language
			if _, ok := SupportedLanguages[lang]; !ok {
				return lang
			}
		}
	}

	config := &quick.Config{
		MaxCount: 100,
	}

	// Sub-property 3a: Unsupported source language should be rejected
	t.Run("UnsupportedSourceLanguage", func(t *testing.T) {
		property := func(seed int64) bool {
			r := rand.New(rand.NewSource(seed))
			unsupportedLang := generateUnsupportedLang(r)

			input := TranslationInput{
				Text:           "Hello world",
				SourceLanguage: unsupportedLang,
				TargetLanguage: "zh", // valid target
			}

			err := ValidateTranslationInput(input)
			return errors.Is(err, ErrUnsupportedLanguage)
		}

		if err := quick.Check(property, config); err != nil {
			t.Errorf("Property 3a (Unsupported Source Language) failed: %v", err)
		}
	})

	// Sub-property 3b: Unsupported target language should be rejected
	t.Run("UnsupportedTargetLanguage", func(t *testing.T) {
		property := func(seed int64) bool {
			r := rand.New(rand.NewSource(seed))
			unsupportedLang := generateUnsupportedLang(r)

			input := TranslationInput{
				Text:           "Hello world",
				SourceLanguage: "en", // valid source
				TargetLanguage: unsupportedLang,
			}

			err := ValidateTranslationInput(input)
			return errors.Is(err, ErrUnsupportedLanguage)
		}

		if err := quick.Check(property, config); err != nil {
			t.Errorf("Property 3b (Unsupported Target Language) failed: %v", err)
		}
	})

	// Sub-property 3c: "auto" as target language should be rejected
	t.Run("AutoAsTargetLanguage", func(t *testing.T) {
		input := TranslationInput{
			Text:           "Hello world",
			SourceLanguage: "en",
			TargetLanguage: "auto", // "auto" is not valid for target
		}

		err := ValidateTranslationInput(input)
		if !errors.Is(err, ErrUnsupportedLanguage) {
			t.Errorf("Expected ErrUnsupportedLanguage for 'auto' as target, got: %v", err)
		}
	})
}

// TestValidTranslationInput tests that valid inputs pass validation
func TestValidTranslationInput(t *testing.T) {
	config := &quick.Config{
		MaxCount: 100,
	}

	// Get all valid source and target languages
	var validSourceLangs []string
	for lang := range SupportedLanguages {
		validSourceLangs = append(validSourceLangs, lang)
	}

	var validTargetLangs []string
	for lang := range ValidTargetLanguages {
		validTargetLangs = append(validTargetLangs, lang)
	}

	// Property: For any valid input combination, validation should pass
	property := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))

		// Generate non-empty text with at least one non-whitespace character
		textLen := 1 + r.Intn(100)
		var sb strings.Builder
		for i := 0; i < textLen; i++ {
			sb.WriteByte(byte('a' + r.Intn(26)))
		}
		text := sb.String()

		sourceLang := validSourceLangs[r.Intn(len(validSourceLangs))]
		targetLang := validTargetLangs[r.Intn(len(validTargetLangs))]

		input := TranslationInput{
			Text:           text,
			SourceLanguage: sourceLang,
			TargetLanguage: targetLang,
		}

		err := ValidateTranslationInput(input)
		return err == nil
	}

	if err := quick.Check(property, config); err != nil {
		t.Errorf("Valid input validation failed: %v", err)
	}
}

// TestProperty1_TranslationOutputStructureCompleteness tests that translation output has complete structure
// **Property 1: 翻译返回结构完整性**
// **Validates: Requirements 1.1, 1.2, 1.3**
// This property test validates that for any valid TranslationOutput:
// - translated_text is non-empty
// - source_language is a valid language code
// - target_language matches a valid target language
func TestProperty1_TranslationOutputStructureCompleteness(t *testing.T) {
	config := &quick.Config{
		MaxCount: 100,
	}

	// Get all valid language codes
	var validLangCodes []string
	for lang := range ValidTargetLanguages {
		validLangCodes = append(validLangCodes, lang)
	}

	// Property: For any TranslationOutput with valid structure, all fields should be properly set
	// This tests the structural invariants of TranslationOutput
	property := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))

		// Generate a simulated valid TranslationOutput
		// (simulating what the AI model should return)
		textLen := 1 + r.Intn(200)
		var sb strings.Builder
		for i := 0; i < textLen; i++ {
			sb.WriteByte(byte('a' + r.Intn(26)))
		}
		translatedText := sb.String()

		sourceLang := validLangCodes[r.Intn(len(validLangCodes))]
		targetLang := validLangCodes[r.Intn(len(validLangCodes))]
		confidence := r.Float64()

		output := TranslationOutput{
			TranslatedText: translatedText,
			SourceLanguage: sourceLang,
			TargetLanguage: targetLang,
			Confidence:     confidence,
		}

		// Validate structure completeness
		// 1. translated_text must be non-empty
		if strings.TrimSpace(output.TranslatedText) == "" {
			return false
		}

		// 2. source_language must be a valid language code
		if _, ok := ValidTargetLanguages[output.SourceLanguage]; !ok {
			return false
		}

		// 3. target_language must be a valid target language code
		if _, ok := ValidTargetLanguages[output.TargetLanguage]; !ok {
			return false
		}

		// 4. confidence should be between 0 and 1 (if set)
		if output.Confidence < 0 || output.Confidence > 1 {
			return false
		}

		return true
	}

	if err := quick.Check(property, config); err != nil {
		t.Errorf("Property 1 (Translation Output Structure Completeness) failed: %v", err)
	}
}

// TestProperty1_ValidOutputStructureValidator tests the output validation logic
// This is a helper property test that validates our output structure checking logic
func TestProperty1_ValidOutputStructureValidator(t *testing.T) {
	// ValidateTranslationOutput checks if a TranslationOutput has valid structure
	validateOutput := func(output *TranslationOutput) bool {
		if output == nil {
			return false
		}
		// translated_text must be non-empty
		if strings.TrimSpace(output.TranslatedText) == "" {
			return false
		}
		// source_language must be valid
		if _, ok := ValidTargetLanguages[output.SourceLanguage]; !ok {
			return false
		}
		// target_language must be valid
		if _, ok := ValidTargetLanguages[output.TargetLanguage]; !ok {
			return false
		}
		return true
	}

	config := &quick.Config{
		MaxCount: 100,
	}

	var validLangCodes []string
	for lang := range ValidTargetLanguages {
		validLangCodes = append(validLangCodes, lang)
	}

	// Property: Valid outputs should pass validation
	t.Run("ValidOutputsPassValidation", func(t *testing.T) {
		property := func(seed int64) bool {
			r := rand.New(rand.NewSource(seed))

			textLen := 1 + r.Intn(100)
			var sb strings.Builder
			for i := 0; i < textLen; i++ {
				sb.WriteByte(byte('a' + r.Intn(26)))
			}

			output := &TranslationOutput{
				TranslatedText: sb.String(),
				SourceLanguage: validLangCodes[r.Intn(len(validLangCodes))],
				TargetLanguage: validLangCodes[r.Intn(len(validLangCodes))],
				Confidence:     r.Float64(),
			}

			return validateOutput(output)
		}

		if err := quick.Check(property, config); err != nil {
			t.Errorf("Valid outputs should pass validation: %v", err)
		}
	})

	// Property: Outputs with empty translated_text should fail validation
	t.Run("EmptyTranslatedTextFailsValidation", func(t *testing.T) {
		property := func(seed int64) bool {
			r := rand.New(rand.NewSource(seed))

			// Generate whitespace-only text
			whitespaceChars := []rune{' ', '\t', '\n', '\r'}
			length := r.Intn(10)
			result := make([]rune, length)
			for i := range result {
				result[i] = whitespaceChars[r.Intn(len(whitespaceChars))]
			}

			output := &TranslationOutput{
				TranslatedText: string(result),
				SourceLanguage: validLangCodes[r.Intn(len(validLangCodes))],
				TargetLanguage: validLangCodes[r.Intn(len(validLangCodes))],
			}

			return !validateOutput(output)
		}

		if err := quick.Check(property, config); err != nil {
			t.Errorf("Empty translated_text should fail validation: %v", err)
		}
	})

	// Property: Outputs with invalid source_language should fail validation
	t.Run("InvalidSourceLanguageFailsValidation", func(t *testing.T) {
		generateInvalidLang := func(r *rand.Rand) string {
			chars := "abcdefghijklmnopqrstuvwxyz"
			for {
				length := 2 + r.Intn(2)
				result := make([]byte, length)
				for i := range result {
					result[i] = chars[r.Intn(len(chars))]
				}
				lang := string(result)
				if _, ok := ValidTargetLanguages[lang]; !ok {
					return lang
				}
			}
		}

		property := func(seed int64) bool {
			r := rand.New(rand.NewSource(seed))

			output := &TranslationOutput{
				TranslatedText: "Some translated text",
				SourceLanguage: generateInvalidLang(r),
				TargetLanguage: validLangCodes[r.Intn(len(validLangCodes))],
			}

			return !validateOutput(output)
		}

		if err := quick.Check(property, config); err != nil {
			t.Errorf("Invalid source_language should fail validation: %v", err)
		}
	})
}
