package starter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/workspace"
)

const dslReference = `
PromptLoom DSL quick reference
================================
Files:
  - .prompt.loom  — prompt definitions
  - .block.loom   — reusable block definitions (mixins)

Prompt syntax:
  prompt Name { ... }
  prompt Child inherits Parent { ... }

Block syntax:
  block Name { ... }

Inside a prompt body:
  use BlockName            — apply a block

Fields (use := to set, += to append, -= to remove):
  kind           — role identifier, e.g. "code-reviewer"
  summary        — one-paragraph description of the prompt
  persona        — who the assistant is
  context        — background info for the assistant
  objective      — what the assistant should achieve
  instructions   — list of specific guidance items (bullet list)
  constraints    — list of hard rules (bullet list)
  examples       — list of usage examples (bullet list)
  format         — expected output sections (bullet list)
  notes          — implementation notes
  todo           — list of pending improvements

Special blocks:
  contract {
    required_sections:
      - Section Name
    must_include:
      - keyword
    must_not_include:
      - forbidden word
  }
  capabilities {
    allowed:
      - read_code
    forbidden:
      - delete_files
  }

Field operators:
  :=   override (replace entire value)
  +=   append to existing value
  -=   remove item from list

Inheritance rules:
  - Child inherits all parent fields
  - Use += to extend, := to replace
  - Blocks applied with "use" merge their fields into the prompt

Example:
  block JavaConventions {
    constraints:
      - Prefer immutable objects.
      - Use Optional instead of null returns.
  }

  prompt BaseEngineer {
    kind := code-assistant
    persona:
      You are a senior engineer.
    objective:
      Help the user write, review, and debug code.
    instructions:
      - Read context before responding.
      - Explain your reasoning.
    constraints:
      - Keep responses focused on the user's question.
    format:
      - Analysis
      - Proposed Changes
  }

  prompt CodeReviewer inherits BaseEngineer {
    use JavaConventions
    persona :=
      You are a senior Java engineer doing a thorough code review.
    instructions +=
      - Check for correctness, edge cases, and error handling.
    format +=
      - Issues Found
      - Verdict
  }
`

func systemPrompt() string {
	return `You are an expert prompt engineer for the PromptLoom DSL. Your task is to generate high-quality, production-ready prompt libraries for software development teams.

` + dslReference + `
Output instructions:
- Return ONLY a valid JSON array. No markdown fences. No explanation. No trailing text.
- Each element must have exactly these fields:
  {
    "name": "PascalCaseName.prompt.loom",  (or .block.loom for blocks)
    "type": "prompt",                       (or "block")
    "description": "one-line description",
    "content": "complete .loom file content as a string"
  }
- Use \\n for newlines inside content strings.
- Make prompts specific to the project's actual stack and TODOs, not generic.
- Build a proper inheritance hierarchy: one base prompt, then specialised children.
- Every prompt must have at minimum: kind, persona, objective, instructions, constraints, format.
- Blocks should focus on reusable rules that multiple prompts would benefit from.`
}

func userPrompt(info *workspace.Info, tier Tier) string {
	var sb strings.Builder

	if info.HasClaudeMD {
		sb.WriteString("=== CLAUDE.md (project context) ===\n")
		sb.WriteString(info.ClaudeMD)
		sb.WriteString("\n\n")
	}

	if info.HasTodoMD {
		sb.WriteString("=== TODO.md (developer tasks) ===\n")
		sb.WriteString(info.TodoMD)
		sb.WriteString("\n\n")
	}

	fmt.Fprintf(&sb, "=== Detected stack ===\n%s\n", info.Summary())

	switch tier {
	case TierMinimal:
		sb.WriteString(`
Generate a minimal but high-value starter pack: exactly 3-5 prompts + 1 block.
Focus on the 3-5 most essential developer workflows for this project.
Keep instructions and constraints concise. Quality over quantity.
`)
	case TierBest:
		sb.WriteString(`
Generate the most comprehensive, highest-quality prompt library possible.
Target: 15-20 prompts + 5-8 blocks.
Requirements:
- Deep inheritance hierarchy (3+ levels where appropriate)
- Detailed, specific instructions and constraints for each role
- contract {} blocks for key prompts
- Multiple specialised reviewers (code, security, performance, docs)
- Prompts that directly address each item in TODO.md
- Blocks for shared conventions, security rules, testing standards
- Maintenance prompts (documentation writer, changelog writer, migration guide)
- Each prompt should have rich context and examples fields
Maximise specificity to this project. Avoid generic placeholders.
`)
	default: // TierDefault
		sb.WriteString(`
Generate a comprehensive starter library:
- 8-12 prompts with a clear inheritance structure
- 2-4 blocks for shared reusable rules
Requirements:
- One base prompt capturing the core tech stack persona
- Role prompts: code reviewer, test writer, documentation writer, security reviewer
- At least one prompt addressing items from TODO.md
- 2-3 blocks for commonly shared rules (language conventions, security checklist, etc.)
- Each prompt: kind, persona, objective, 4-6 instructions, 3-5 constraints, format
Moderate detail — enough to be useful immediately, not so long as to be noisy.
`)
	}

	return sb.String()
}

// GenerateLLM calls the configured LLM to produce a starter Plan.
func GenerateLLM(info *workspace.Info, cfg *config.Config, tier Tier) (*Plan, error) {
	apiKey, err := resolveKey(cfg)
	if err != nil {
		return nil, err
	}

	timeout := 120 * time.Second
	if tier == TierBest {
		timeout = 180 * time.Second
	}

	provider := cfg.Testing.Provider
	if provider == "" {
		provider = "gemini"
	}
	model := cfg.Testing.DefaultModel
	if model == "" {
		model = "gemini-2.5-flash"
	}

	raw, err := callModel(provider, model, apiKey, systemPrompt(), userPrompt(info, tier), timeout)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return parseJSONPlan(raw)
}

func resolveKey(cfg *config.Config) (string, error) {
	envVar := cfg.Testing.APIKeyEnv
	if envVar == "" {
		switch strings.ToLower(cfg.Testing.Provider) {
		case "anthropic":
			envVar = "ANTHROPIC_API_KEY"
		default:
			envVar = "GEMINI_API_KEY"
		}
	}
	key := os.Getenv(envVar)
	if key == "" {
		return "", fmt.Errorf("API key not set: $%s is empty\nAdd it to .loom.secret or export it in your shell", envVar)
	}
	return key, nil
}

func parseJSONPlan(raw string) (*Plan, error) {
	// Strip markdown code fences if the model wrapped the JSON anyway.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Find the JSON array boundaries.
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("LLM response does not contain a JSON array\n\nRaw response (first 500 chars):\n%s", truncate(raw, 500))
	}
	raw = raw[start : end+1]

	var items []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("failed to parse LLM JSON: %w\n\nRaw (first 500 chars):\n%s", err, truncate(raw, 500))
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("LLM returned an empty file list")
	}

	plan := &Plan{}
	for _, item := range items {
		if item.Name == "" || item.Content == "" {
			continue
		}
		typ := item.Type
		if typ == "" {
			if strings.Contains(item.Name, ".block.") {
				typ = "block"
			} else {
				typ = "prompt"
			}
		}
		plan.Files = append(plan.Files, File{
			Name:        item.Name,
			Type:        typ,
			Description: item.Description,
			Content:     item.Content,
		})
	}
	if len(plan.Files) == 0 {
		return nil, fmt.Errorf("LLM returned no usable files")
	}
	return plan, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// --- shared HTTP client helpers (mirrored from testrunner) ---

func callModel(provider, model, apiKey, sysPrompt, userIn string, timeout time.Duration) (string, error) {
	switch strings.ToLower(provider) {
	case "anthropic":
		return callAnthropic(model, apiKey, sysPrompt, userIn, timeout)
	default:
		return callGemini(model, apiKey, sysPrompt, userIn, timeout)
	}
}

// --- Gemini ---

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}
type geminiPart struct {
	Text string `json:"text"`
}
type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func callGemini(model, apiKey, sysPrompt, userIn string, timeout time.Duration) (string, error) {
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, apiKey,
	)
	req := geminiRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: sysPrompt}}},
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userIn}}},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}
	if gr.Error != nil {
		return "", fmt.Errorf("Gemini API error %d: %s", gr.Error.Code, gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini returned no content")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}

// --- Anthropic ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func callAnthropic(model, apiKey, sysPrompt, userIn string, timeout time.Duration) (string, error) {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	req := anthropicRequest{
		Model:     model,
		MaxTokens: 8192,
		System:    sysPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: userIn}},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var ar anthropicResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("Anthropic API error (%s): %s", ar.Error.Type, ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return "", fmt.Errorf("Anthropic returned no content")
	}
	return ar.Content[0].Text, nil
}
