PromptLoom test fixtures
========================

Each directory under valid/ and invalid/ is a tiny PromptLoom project. The tests in
internal/e2e copy it to a temp dir and run the real pipeline:

    loader -> validate -> resolve -> render     (and the built `loom` binary, see cli_test.go)

If a fixture has no loom.toml, a quiet default is used (missing objective/format are
NOT reported) so a fixture only mentions what it is testing.

valid/<case>/
    prompts/ blocks/ overlays/ loompack/   the project
    expect.txt   (optional)  warnings the project is expected to produce
    weave.txt    (optional)  what to render; default = every prompt to Markdown
    golden/      expected output, one file per weave line
    NOTE.txt     (optional)  human notes, ignored by the tests

invalid/<case>/
    the project, plus:
    expect.txt   REQUIRED    what must go wrong
    weave.txt    (optional)  weave attempts that must fail (for weave-error)

expect.txt lines (blank lines and # comments are ignored)
    rule: <id>            which rule this fixture demonstrates (must be listed in allRules)
    error: <text>         a validation error whose message contains <text>
    warning: <text>       a validation warning whose message contains <text>
    load-error: <text>    the project fails to load (parse error, duplicate name, ...)
    weave-error: <text>   a weave attempt from weave.txt fails with <text>
    also: <text>          <text> appears somewhere in the output (e.g. a "Did you mean" hint)

Matching is exact in both directions: every diagnostic must be expected and every
expectation must be met, so an unexpected new warning fails the test.
Every diagnostic must also carry a file and line.

weave.txt lines
    <golden-name>: <PromptName> [--variant V] [--env E] [--overlay O]... [--set k=v]... [--format F]
    A golden name without an extension gets ".md".

Adding a rule
    1. add its id to allRules in internal/e2e/e2e_test.go
    2. add invalid/<id>/ with expect.txt
    TestEveryRuleHasAFixture fails until you do.

Updating goldens after an intentional output change
    go test ./internal/e2e -update
    then read the diff of testdata/valid/*/golden before committing.
