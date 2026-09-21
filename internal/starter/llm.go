package starter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/workspace"
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

Fields (set every field with :=):
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

Field operator (there is exactly ONE):
  :=   set the field. The value goes on the following indented lines.
       Never use  +=  or  -=  or a bare  field:  — they are not valid.

Inheritance rules:
  - A child inherits all parent fields; write a field with := to replace it.
  - To EXTEND a parent's list, use from(parent[0]) and { ... } (lists only):
        instructions :=
          from(parent[0]) and {
            - one more step
          }
    with several parents use from(parent[*]) to merge all of them.
  - Scalar fields (persona, objective, context, summary, notes, kind) can only be
    replaced, or copied with  persona :=  from(parent[0]).
  - Blocks applied with "use" ADD their list items to the prompt (format is replaced).

Example:
  block JavaConventions {
    constraints :=
      - Prefer immutable objects.
      - Use Optional instead of null returns.
  }

  prompt BaseEngineer {
    kind :=
      code-assistant
    persona :=
      You are a senior engineer.
    objective :=
      Help the user write, review, and debug code.
    instructions :=
      - Read context before responding.
      - Explain your reasoning.
    constraints :=
      - Keep responses focused on the user's question.
    format :=
      - Analysis
      - Proposed Changes
  }

  prompt CodeReviewer inherits BaseEngineer {
    use JavaConventions
    persona :=
      You are a senior Java engineer doing a thorough code review.
    instructions :=
      from(parent[0]) and {
        - Check for correctness, edge cases, and error handling.
      }
    format :=
      from(parent[0]) and {
        - Issues Found
        - Verdict
      }
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

// GenerateLLM calls the configured LLM to produce a starter Plan. The provider, key and model
// come from [testing] in loom.toml, through the shared client (internal/llm).
func GenerateLLM(info *workspace.Info, cfg *config.Config, tier Tier) (*Plan, error) {
	client, err := llm.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	client.Timeout = 120 * time.Second
	if tier == TierBest {
		client.Timeout = 180 * time.Second
	}

	raw, err := client.Complete(context.Background(), llm.Request{System: systemPrompt(), User: userPrompt(info, tier)})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}
	return parseJSONPlan(raw)
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
