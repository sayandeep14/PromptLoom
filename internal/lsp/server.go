package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Server is the LSP server instance.
type Server struct {
	in  *bufio.Reader
	out *bufio.Writer
	mu  sync.Mutex

	docs  map[string]string // uri → full text
	roots map[string]string // uri → project root dir
}

// New creates a new Server reading from r and writing to w.
func New(r io.Reader, w io.Writer) *Server {
	return &Server{
		in:    bufio.NewReader(r),
		out:   bufio.NewWriter(w),
		docs:  make(map[string]string),
		roots: make(map[string]string),
	}
}

// Run starts the JSON-RPC message loop. Returns when stdin closes.
func (s *Server) Run() error {
	for {
		msg, err := s.readMessage()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.dispatch(msg)
	}
}

func (s *Server) readMessage() (*rpcMessage, error) {
	contentLength := 0
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			n, _ := strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
			contentLength = n
		}
	}
	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(s.in, body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Server) writeMessage(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(data))
	s.out.Write(data)
	s.out.Flush()
}

func (s *Server) respond(id interface{}, result interface{}) {
	s.writeMessage(rpcMessage{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) respondError(id interface{}, code int, message string) {
	s.writeMessage(rpcMessage{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (s *Server) notify(method string, params interface{}) {
	data, _ := json.Marshal(params)
	s.writeMessage(rpcMessage{JSONRPC: "2.0", Method: method, Params: data})
}

func (s *Server) dispatch(msg *rpcMessage) {
	switch msg.Method {
	case "initialize":
		var p InitializeParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleInitialize(msg.ID, p)
	case "initialized":
		// nothing
	case "shutdown":
		s.respond(msg.ID, nil)
	case "exit":
		os.Exit(0)
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleDidOpen(p)
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleDidChange(p)
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleDidClose(p)
	case "textDocument/hover":
		var p TextDocumentPositionParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleHover(msg.ID, p)
	case "textDocument/definition":
		var p TextDocumentPositionParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleDefinition(msg.ID, p)
	case "textDocument/completion":
		var p CompletionParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleCompletion(msg.ID, p)
	case "textDocument/documentSymbol":
		var p DocumentSymbolParams
		_ = json.Unmarshal(msg.Params, &p)
		s.handleDocumentSymbol(msg.ID, p)
	default:
		if msg.ID != nil {
			s.respondError(msg.ID, -32601, "method not found: "+msg.Method)
		}
	}
}

// ---- helpers ----

func uriToPath(uri string) string {
	return strings.ReplaceAll(strings.TrimPrefix(uri, "file://"), "%20", " ")
}

func pathToURI(path string) string {
	return "file://" + path
}

func findRoot(path string) string {
	dir := path
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "loom.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Dir(path)
}

func getLine(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return lines[line]
}

func wordAt(line string, ch int) (word string, start, end int) {
	if ch > len(line) {
		ch = len(line)
	}
	s := ch
	for s > 0 && isIdentRune(rune(line[s-1])) {
		s--
	}
	e := ch
	for e < len(line) && isIdentRune(rune(line[e])) {
		e++
	}
	return line[s:e], s, e
}

func isIdentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}
