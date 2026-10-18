package translate

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/genai"
)

// DefaultModel is the Gemini model used unless configured otherwise.
const DefaultModel = "gemini-3.1-flash-lite"

//go:embed system_prompt.txt
var systemPrompt string

// Gemini is a client for Google's Gemini API.
type Gemini struct {
	client *genai.Client
	model  string
}

// NewGemini creates a Gemini client. The client is created lazily by the API,
// so this does not perform any network calls.
func NewGemini(ctx context.Context, apiKey, model string) (*Gemini, error) {
	if apiKey == "" {
		return nil, errors.New("no Gemini API key configured")
	}
	if model == "" {
		model = DefaultModel
	}
	return newGemini(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI}, model)
}

func newGemini(ctx context.Context, cc *genai.ClientConfig, model string) (*Gemini, error) {
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, err
	}
	return &Gemini{client: client, model: model}, nil
}

func (g *Gemini) generate(ctx context.Context, prompt string, config *genai.GenerateContentConfig) (string, error) {
	resp, err := g.client.Models.GenerateContent(ctx, g.model, genai.Text(prompt), config)
	if err != nil {
		return "", fmt.Errorf("gemini: %w", err)
	}
	if u := resp.UsageMetadata; u != nil {
		slog.Info("gemini usage", "model", g.model, "prompt", u.PromptTokenCount,
			"thinking", u.ThoughtsTokenCount, "response", u.CandidatesTokenCount, "total", u.TotalTokenCount)
	}
	text := resp.Text()
	if text == "" {
		return "", errEmptyResponse
	}
	return text, nil
}

var errEmptyResponse = errors.New("gemini: empty response")

// Monolithic translates a whole request in a single structured model call;
// the system prompt instructs the model to pick verbatim substrings of the
// authoritative references.
type Monolithic struct{ *Gemini }

var responseSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"paragraphs": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"text": {Type: genai.TypeString},
					"id":   {Type: genai.TypeString},
				},
				Required: []string{"text"},
			},
		},
	},
	Required: []string{"paragraphs"},
}

func (m Monolithic) Translate(ctx context.Context, req ModelRequest) (*ModelResponse, error) {
	prompt, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	slog.Debug("translation prompt", "prompt", string(prompt))
	text, err := m.generate(ctx, string(prompt), &genai.GenerateContentConfig{
		ResponseMIMEType:  "application/json",
		ResponseSchema:    responseSchema,
		ThinkingConfig:    &genai.ThinkingConfig{ThinkingBudget: genai.Ptr[int32](0)},
		SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
	})
	if err != nil {
		return nil, err
	}
	slog.Debug("translation response", "response", text)
	var resp ModelResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("decoding model response: %w", err)
	}
	return &resp, nil
}

// ParagraphTranslator translates a single paragraph from scratch.
type ParagraphTranslator interface {
	TranslateParagraph(ctx context.Context, fromLanguage, toLanguage, text string) (string, error)
}

// TextMatcher finds the part of a reference translation that corresponds to
// a source text.
type TextMatcher interface {
	FindBestMatch(ctx context.Context, fromLanguage, toLanguage, source, reference string) (string, error)
}

// GeminiParagraphTranslator is a ParagraphTranslator backed by Gemini.
type GeminiParagraphTranslator struct{ *Gemini }

func (g GeminiParagraphTranslator) TranslateParagraph(ctx context.Context, fromLanguage, toLanguage, text string) (string, error) {
	return g.generate(ctx, fmt.Sprintf("Translate the following text from %s to %s: %s", fromLanguage, toLanguage, text), nil)
}

// GeminiTextMatcher is a TextMatcher backed by Gemini.
type GeminiTextMatcher struct{ *Gemini }

func (g GeminiTextMatcher) FindBestMatch(ctx context.Context, fromLanguage, toLanguage, source, reference string) (string, error) {
	prompt := fmt.Sprintf(`Given the following text in %s:
---
%s
---

And the following reference translation in %s:
---
%s
---

Please identify and return the portion of the reference translation that is the most accurate translation of the source text. Only return the translated text, with no additional explanation or commentary.`,
		fromLanguage, source, toLanguage, reference)
	text, err := g.generate(ctx, prompt, nil)
	if errors.Is(err, errEmptyResponse) {
		return "", nil // no match; the caller translates from scratch
	}
	return text, err
}

// Incremental translates paragraph by paragraph: paragraphs with a reference
// are matched against it, others (or failed matches) translated from scratch.
type Incremental struct {
	Translator ParagraphTranslator
	Matcher    TextMatcher
}

func (inc Incremental) Translate(ctx context.Context, req ModelRequest) (*ModelResponse, error) {
	slog.Info("incremental translation", "from", req.FromLanguage, "to", req.ToLanguage, "paragraphs", len(req.Paragraphs))
	resp := &ModelResponse{Paragraphs: make([]ParagraphWithID, 0, len(req.Paragraphs))}
	for _, p := range req.Paragraphs {
		var text string
		var err error
		if p.Reference != nil {
			text, err = inc.Matcher.FindBestMatch(ctx, req.FromLanguage, req.ToLanguage, p.Text, p.Reference.Text)
			if err != nil {
				return nil, err
			}
		}
		if text == "" {
			if text, err = inc.Translator.TranslateParagraph(ctx, req.FromLanguage, req.ToLanguage, p.Text); err != nil {
				return nil, err
			}
		}
		out := ParagraphWithID{Text: text}
		if p.Reference != nil {
			out.ID = p.Reference.ID
		}
		resp.Paragraphs = append(resp.Paragraphs, out)
	}
	return resp, nil
}
