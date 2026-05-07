// Package lsp implements a Language Server Protocol server for .loom files.
package lsp

import "encoding/json"

// ---- JSON-RPC 2.0 ----

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- LSP core types ----

type Position struct {
	Line      int `json:"line"`      // 0-indexed
	Character int `json:"character"` // 0-indexed
}

type LSPRange struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string   `json:"uri"`
	Range LSPRange `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// ---- initialize ----

type InitializeParams struct {
	RootURI string `json:"rootUri"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

type ServerCapabilities struct {
	TextDocumentSync       int                `json:"textDocumentSync"`
	HoverProvider          bool               `json:"hoverProvider"`
	DefinitionProvider     bool               `json:"definitionProvider"`
	CompletionProvider     *CompletionOptions `json:"completionProvider,omitempty"`
	DocumentSymbolProvider bool               `json:"documentSymbolProvider"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ---- textDocument/didOpen ----

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// ---- textDocument/didChange ----

type DidChangeTextDocumentParams struct {
	TextDocument   TextDocumentIdentifier           `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// ---- textDocument/didClose ----

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// ---- publishDiagnostics ----

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range    LSPRange `json:"range"`
	Severity int      `json:"severity"` // 1=Error 2=Warning 3=Info 4=Hint
	Message  string   `json:"message"`
	Source   string   `json:"source,omitempty"`
}

// ---- hover ----

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *LSPRange     `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// ---- completion ----

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"` // 14=Keyword 5=Field 9=Module
	Detail        string `json:"detail,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
	Documentation string `json:"documentation,omitempty"`
}

// ---- documentSymbol ----

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"` // 5=Field 6=Variable 12=Function
	Range          LSPRange         `json:"range"`
	SelectionRange LSPRange         `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}
