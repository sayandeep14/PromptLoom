  ---                                                                                                                            
  Lumine — Implementation Plan
                                                                                                                                 
  Each phase is a discrete, testable unit we'll complete before moving to the next.
                                                                                                                                 
  ---                                                                                                                            
  Phase 1 — Project Scaffold & Syntax Highlighting                                                                               
                                                                                                                                 
  Goal: A working, installable extension that colorizes .loom files.
                                                                                                                                 
  1. Initialize the extension project (package.json, tsconfig.json, esbuild build script)                                        
  2. Create language-configuration.json (bracket matching, comment toggle, {{ }} auto-close)                                     
  3. Create syntaxes/loom.tmLanguage.json — all 12 TextMate grammar patterns (keywords, field operators, {{ }} tokens, list      
  bullets, etc.)                                                                                                                 
  4. Register language + grammar in package.json                                                                                 
  5. Write a minimal src/extension.ts (no LSP yet, just activates)                                                               
  6. Wire up esbuild build and test by pressing F5 in VS Code                                                                    
                                                                                                                                 
  Done when: Opening any .loom sample file shows correct syntax colors.                                                          
                                                                                                                                 
  ---                                                                                                                            
  Phase 2 — Snippets
                    
  Goal: All 11 snippets available for fast authoring.
                                                                                                                                 
  1. Create snippets/loom.json with all snippet definitions (prompt, prompti, block, overlay, use, var, slot, variant, contract, 
  capabilities, {{)                                                                                                              
  2. Register in package.json                                                                                                    
                                                                                                                                 
  Done when: Typing prompt in a .loom file triggers a snippet.                                                                   
                                                                                                                                 
  ---                                                                                                                            
  Phase 3 — LSP Skeleton + Document Symbols
                                                                                                                                 
  Goal: Language Server wired up; Outline panel shows prompt/block/overlay names.
                                                                                                                                 
  1. Add LSP dependencies to package.json (vscode-languageserver, vscode-languageclient)                                         
  2. Create src/server/server.ts — LSP server entry point with onInitialize                                                      
  3. Update src/extension.ts to launch the server via IPC                                                                        
  4. Implement onDocumentSymbol using a stub parser (regex-level, good enough for names)                                         
  5. Add loom.trace.server config + validateOnSave/formatOnSave settings to package.json                                         
                                                                                                                                 
  Done when: The Outline panel lists all declarations by name.                                                                   
                                                                                                                                 
  ---                                                                                                                            
  Phase 4 — Full DSL Parser
                           
  Goal: A complete, lenient TypeScript parser that produces typed AST nodes.
                                                                                                                                 
  1. Implement src/server/parser.ts — parseLoomDocument(text, uri): ParseResult                                                  
    - Tokenizer / line-by-line state machine                                                                                     
    - Handles all node types: prompt, block, overlay                                                                             
    - Handles inherits, use, var, slot, variant, contract, capabilities
    - Captures Range for every named token (needed for go-to-def, rename)                                                        
    - Error recovery — produces partial results on incomplete input                                                              
  2. Implement parseVarsFile() for .vars.loom top-level-only format                                                              
  3. Replace the stub parser in the server with the full parser                                                                  
                                                                                                                                 
  Done when: Parser correctly produces AST for all sample files in Part 1.10 of the spec, including edge cases.                  
                  
  ---                                                                                                                            
  Phase 5 — Registry + Real-Time Error Diagnostics
                                                                                                                                 
  Goal: Inline red squiggles for hard errors as you type.
                                                                                                                                 
  1. Implement src/server/registry.ts — LoomRegistry class (all prompt/block/overlay maps, global vars)                          
  2. On server init: scan workspace with connection.workspace.findFiles to seed the registry                                     
  3. Hook textDocument/didOpen, didChange, didSave, didClose → update registry                                                   
  4. Implement src/server/validator.ts — all hard errors from Part 3:                                                            
    - Unknown parent / unknown block                                                                                             
    - Inheritance cycle                                                                                                          
    - Invalid field name                                                                                                         
    - -= on scalar field                                                                                                         
    - Undefined {{ variable }} token
    - Duplicate names                                                                                                            
  5. Debounce validation (300 ms), push diagnostics via connection.sendDiagnostics
                                                                                                                                 
  Done when: Writing use NonExistentBlock shows a red error; fixing it clears it.                                                
                                                                                                                                 
  ---                                                                                                                            
  Phase 6 — IntelliSense Completions
                                                                                                                                 
  Goal: Smart completions for every context defined in Part 9.
                                                                                                                                 
  1. Implement src/providers/completion.ts — detect cursor context (top-level, body, after inherits, after use, inside {{ }},    
  inside contract {}, inside capabilities {})                                                                                    
  2. Return appropriate CompletionItem lists for each context (prompt names, block names, field names + operators, variable      
  names)                                                                                                                         
  3. Register with connection.onCompletion and onCompletionResolve
                                                                                                                                 
  Done when: All 9 completion contexts from the spec work correctly.                                                             
                                                                                                                                 
  ---                                                                                                                            
  Phase 7 — Hover Documentation
                                                                                                                                 
  Goal: Tooltip explanations for every hoverable token.
                                                                                                                                 
  1. Implement src/providers/hover.ts — match cursor position to:                                                                
    - Prompt name in inherits → show fields defined, inherited-by list
    - Block name in use → show fields, used-by list                                                                              
    - Field name → description + current operator semantics                                                                      
    - Operator (:, :=, +=, -=) → operator reference                                                                              
    - {{ varName }} → var/slot details (default value, required status)                                                          
  2. Register with connection.onHover                                                                                            
                                                                                                                                 
  Done when: Hovering over any keyword, field, or token shows a useful tooltip.                                                  
                                                                                                                                 
  ---                                                                                                                            
  Phase 8 — Go-to-Definition + Find References
                                              
  Goal: Ctrl+click navigation and Find All References.
                                                                                                                                 
  1. Implement src/providers/definition.ts — resolve parent names, block names, var tokens to their declaration Location         
  2. Implement src/providers/references.ts — find all usages of a declared prompt, block, or variable                            
  3. Register both with the connection                                                                                           
                  
  Done when: Ctrl+clicking a parent name in inherits jumps to its definition file.                                               
                  
  ---                                                                                                                            
  Phase 9 — Auto-Formatter
                          
  Goal: Format Document produces canonical Loom output, matching loom fmt.
                                                                                                                                 
  1. Implement src/server/formatter.ts — formatNodes(nodes): string following all 8 rules from Part 1.11                         
  2. Register connection.onDocumentFormatting — skip if parse errors exist                                                       
  3. Honor loom.formatOnSave setting                                                                                             
                  
  Done when: A messy .loom file formats identically to what loom fmt would produce.                                              
                  
  ---                                                                                                                            
  Phase 10 — Warning Diagnostics (loom.toml-Aware)
                                                                                                                                 
  Goal: Configurable warnings matching CLI behavior.
                                                                                                                                 
  1. Implement loom.toml reader — parse the [validation] section for all warning knobs                                           
  2. Add all warnings from Part 3 to the validator:                                                                              
    - Missing objective/format/contract (if configured)                                                                          
    - Deep inheritance chain                                                                                                     
    - Ambiguous : on inherited field                                                                                             
    - Block variables not declared in consuming prompt                                                                           
  3. Re-run validation when loom.toml changes                                                                                    
                                                                                                                                 
  Done when: Warning squiggles appear/disappear based on loom.toml config.                                                       
                                                                                                                                 
  ---             
  Phase 11 — loom.toml Completions & Hover
                                                                                                                                 
  Goal: IntelliSense when editing loom.toml.
                                                                                                                                 
  1. Register a completion provider scoped to files named loom.toml                                                              
  2. Provide section headers, known keys per section, types, and defaults                                                        
  3. Provide hover docs for each key                                                                                             
                  
  Done when: Typing inside loom.toml suggests valid keys with documentation.                                                     
                  
  ---                                                                                                                            
  Phase 12 — CLI Commands
                                                                                                                                 
  Goal: loom weave, loom inspect, loom graph callable from the Command Palette.
                                                                                                                                 
  1. Register the three commands in package.json
  2. Implement handlers in src/extension.ts — open a terminal, run loom <cmd>                                                    
  3. Add loom.loomExecutable setting for custom binary path                                                                      
                                                                                                                                 
  Done when: Command Palette → "Loom: Weave" runs loom weave in the integrated terminal.                                         
                                                                                                                                 
  ---                                                                                                                            
  Phase 13 — File Icons & Polish
                                                                                                                                 
  Goal: Custom icons for each .loom file type, extension README, CHANGELOG.
                                                                                                                                 
  1. Create SVG icons for prompt/block/overlay/vars/mixed/config                                                                 
  2. Create icons/loom-icon-theme.json mapping extensions to icons                                                               
  3. Register the icon theme in package.json                                                                                     
  4. Write README.md with feature list and screenshots
  5. Write CHANGELOG.md                                                                                                          
                  
  Done when: File Explorer shows distinct icons for each .loom file type.                                                        
                  
  