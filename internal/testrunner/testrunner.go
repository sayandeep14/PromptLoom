// Package testrunner sends rendered prompts to a model and asserts contract rules.
package testrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/contract"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/render"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
)

// Result is the outcome of a single test run.
type Result struct {
	PromptName string
	Passed     bool
	Skipped    bool
	SkipReason string
	Failures   []contract.Failure
	Response   string
	Duration   time.Duration
	Err        error
}

// Options controls a test run.
type Options struct {
	Model    string
	Record   bool
	Compare  bool
	TestsDir string
}

// RunAll runs tests for every prompt in the registry that has a contract.
func RunAll(reg *registry.Registry, cfg *config.Config, cwd string, opts Options) []Result {
	nodes := reg.Prompts()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	var results []Result
	for _, node := range nodes {
		results = append(results, Run(node.Name, reg, cfg, cwd, opts))
	}
	return results
}

// Run runs the test for a single named prompt.
func Run(name string, reg *registry.Registry, cfg *config.Config, cwd string, opts Options) Result {
	start := time.Now()
	res := Result{PromptName: name}

	node, ok := reg.LookupPrompt(name)
	if !ok {
		res.Err = fmt.Errorf("prompt %q not found", name)
		return res
	}

	if node.Contract == nil {
		res.Skipped = true
		res.SkipReason = "no contract declared"
		return res
	}

	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		res.Err = fmt.Errorf("resolve: %w", err)
		return res
	}

	renderedPrompt := render.Render(rp, cfg)

	testsDir := opts.TestsDir
	if testsDir == "" {
		testsDir = filepath.Join(cwd, "tests")
	}
	input := loadFixture(testsDir, name)

	// The provider, key and default model come from [testing] in loom.toml; --model overrides
	// the model for this run.
	client, err := llm.FromConfig(cfg)
	if err != nil {
		res.Err = err
		return res
	}
	if opts.Model != "" {
		client.Model = opts.Model
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}

	response, err := client.Complete(context.Background(), llm.Request{System: renderedPrompt, User: input})
	if err != nil {
		res.Err = fmt.Errorf("model call failed: %w", err)
		return res
	}
	res.Response = response

	if opts.Record {
		if err := writeBaseline(testsDir, name, response); err != nil {
			res.Err = fmt.Errorf("record baseline: %w", err)
			return res
		}
	}

	if opts.Compare {
		baseline, err := readBaseline(testsDir, name)
		if err != nil {
			res.Err = fmt.Errorf("read baseline: %w", err)
			return res
		}
		failures := compareResponses(node.Contract, baseline, response)
		res.Failures = failures
		res.Passed = len(failures) == 0
		res.Duration = time.Since(start)
		return res
	}

	failures := contract.Check(node.Contract, response)
	res.Failures = failures
	res.Passed = len(failures) == 0
	res.Duration = time.Since(start)
	return res
}

func loadFixture(testsDir, name string) string {
	path := filepath.Join(testsDir, name+".input.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultStub
	}
	return string(data)
}

func writeBaseline(testsDir, name, response string) error {
	path := filepath.Join(testsDir, name+".baseline.md")
	// a namespaced prompt name ("team/Reviewer") maps to a sub-directory
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(response), 0644)
}

func readBaseline(testsDir, name string) (string, error) {
	path := filepath.Join(testsDir, name+".baseline.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("baseline not found at %s — run with --record first", path)
	}
	return string(data), nil
}

// compareResponses checks if the new response passes the same contract as the baseline.
// Returns failures relative to the declared contract; also notes if response diverged.
func compareResponses(c *ast.ContractBlock, baseline, current string) []contract.Failure {
	baselineFailures := contract.Check(c, baseline)
	currentFailures := contract.Check(c, current)

	var extra []contract.Failure
	for _, f := range currentFailures {
		found := false
		for _, bf := range baselineFailures {
			if bf.Kind == f.Kind && bf.Detail == f.Detail {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, f)
		}
	}
	return extra
}

const defaultStub = `Please review the following code snippet for issues:

` + "```" + `java
public class UserService {
    public User findUser(String id) {
        String query = "SELECT * FROM users WHERE id = " + id;
        return db.execute(query);
    }
}
` + "```" + `

Identify any problems and suggest fixes.`
