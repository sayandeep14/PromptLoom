package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// maxMessageBytes bounds one incoming message, so a bad Content-Length cannot exhaust memory.
const maxMessageBytes = 64 << 20

// Server is the LSP server instance.
type Server struct {
	in  *bufio.Reader
	out *bufio.Writer
	mu  sync.Mutex

	docs  map[string]string // uri → full text
	roots map[string]string // uri → project root dir

	shutdown bool // a "shutdown" request was received
	exit     bool // an "exit" notification was received
}

// errExitWithoutShutdown is returned by Run when the client sent "exit" without "shutdown"
// first; the LSP spec asks for a non-zero exit code in that case.
var errExitWithoutShutdown = errors.New("lsp: exit received without a prior shutdown")

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
			var bad *badJSONError
			if errors.As(err, &bad) {
				// One malformed message must not end the session; the framing is still intact.
				s.respondError(nil, -32700, "parse error: "+bad.err.Error())
				continue
			}
			return err
		}
		s.dispatch(msg)
		if s.exit {
			if s.shutdown {
				return nil
			}
			return errExitWithoutShutdown
		}
	}
}

// badJSONError marks a message whose body was read but is not valid JSON.
type badJSONError struct{ err error }

func (e *badJSONError) Error() string { return e.err.Error() }

func (s *Server) readMessage() (*rpcMessage, error) {
	contentLength := -1
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		// header names are case-insensitive
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", strings.TrimSpace(value))
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	if contentLength > maxMessageBytes {
		return nil, fmt.Errorf("message of %d bytes exceeds the %d byte limit", contentLength, maxMessageBytes)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(s.in, body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, &badJSONError{err}
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

// rpcResult is a success response. Result has no omitempty: a response with nothing to
// return must still carry "result": null, or clients treat it as malformed.
type rpcResult struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result"`
}

func (s *Server) respond(id interface{}, result interface{}) {
	s.writeMessage(rpcResult{JSONRPC: "2.0", ID: id, Result: result})
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
		s.shutdown = true
		s.respond(msg.ID, nil)
	case "exit":
		s.exit = true
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
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return strings.TrimPrefix(uri, "file://")
	}
	p := u.Path
	// file:///C:/dir/x → C:/dir/x
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths: C:/x → /C:/x
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
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

// LSP columns count UTF-16 code units, but Go strings are indexed by byte; these convert
// between the two so hover, definition and completion land on the right character in lines
// that contain non-ASCII text.
func utf16ToByte(line string, col int) int {
	units := 0
	for i, r := range line {
		if units >= col {
			return i
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return len(line)
}

func byteToUTF16(line string, b int) int {
	if b > len(line) {
		b = len(line)
	}
	units := 0
	for _, r := range line[:b] {
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return units
}

// wordAt finds the identifier around byte offset ch of line.
func wordAt(line string, ch int) (word string, start, end int) {
	if ch > len(line) {
		ch = len(line)
	}
	if ch < 0 {
		ch = 0
	}
	s := ch
	for s > 0 && isIdentByte(line[s-1]) {
		s--
	}
	e := ch
	for e < len(line) && isIdentByte(line[e]) {
		e++
	}
	return line[s:e], s, e
}

// isIdentByte: identifiers are ASCII, so non-ASCII bytes (utf8 continuation included) end a word.
func isIdentByte(b byte) bool {
	return b < utf8.RuneSelf && isIdentRune(rune(b))
}

func isIdentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}
