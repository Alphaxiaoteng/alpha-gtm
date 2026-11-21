// Package mcp implements an Alpha-Nexus MCP server modeled after
// https://github.com/xueyc1f/turbopush-mcp (stdio JSON-RPC + friendly tools).
//
// Architecture:
//
//	Claude / OpenCode / Cursor
//	  │  MCP (stdio, Content-Length framing)
//	  ▼
//	alpha-nexus-egolite agent mcp-serve
//	  │  SQLite + tools.Registry (no external TurboPush HTTP)
//	  ▼
//	~/.alpha-nexus
//
// Dependency-free: protocol implemented in-process (no mark3labs/mcp-go),
// so builds stay offline-friendly. Tool names mirror turbopush-mcp.
package mcp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tools"
)

// Config binds the MCP process to a local data dir / tool runtime.
type Config struct {
	DataDir string
	Runtime *tools.Runtime
	// ExtraRegister adds more tools (e.g. agent schemes) without import cycles.
	ExtraRegister func(s *Server)
}

// ToolDef is one MCP tool.
type ToolDef struct {
	Name        string
	Description string
	// SideEffect is read | write | browser | network. The external stdio
	// projection fails closed unless the value is exactly read.
	SideEffect string
	// InputSchema is JSON Schema object for tools/list
	InputSchema map[string]any
	Handler     ToolHandler
}

// ToolHandler executes a tool call.
type ToolHandler func(ctx context.Context, args map[string]any) (any, error)

// Server is a simple MCP tool registry + stdio loop.
type Server struct {
	name         string
	version      string
	instructions string
	tools        map[string]ToolDef
	order        []string
	dataDir      string
}

// NewServer creates an empty MCP server shell.
func NewServer(name, version, instructions string) *Server {
	return &Server{
		name: name, version: version, instructions: instructions,
		tools: map[string]ToolDef{},
	}
}

// AddTool registers a tool (last wins on name clash).
func (s *Server) AddTool(def ToolDef) {
	if def.Name == "" || def.Handler == nil {
		return
	}
	if _, ok := s.tools[def.Name]; !ok {
		s.order = append(s.order, def.Name)
	}
	if def.InputSchema == nil {
		def.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	s.tools[def.Name] = def
}

// restrictToReadOnly removes every unclassified or side-effecting tool from
// the external MCP projection. It runs after ExtraRegister so extensions
// cannot accidentally reopen write/browser/network access.
func (s *Server) restrictToReadOnly() {
	order := make([]string, 0, len(s.order))
	for _, name := range s.order {
		def, ok := s.tools[name]
		if !ok || !isMCPReadSideEffect(def.SideEffect) {
			delete(s.tools, name)
			continue
		}
		order = append(order, name)
	}
	s.order = order
}

func sanitizeMCPOutput(out any) (any, error) {
	if out == nil {
		return nil, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(b, &value); err != nil {
		return nil, err
	}
	return sanitizeMCPValue(value), nil
}

func sanitizeMCPValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(v))
		for key, item := range v {
			if isSensitiveMCPResponseKey(key) {
				continue
			}
			clean[key] = sanitizeMCPValue(item)
		}
		return clean
	case []any:
		clean := make([]any, len(v))
		for i := range v {
			clean[i] = sanitizeMCPValue(v[i])
		}
		return clean
	case string:
		if filepath.IsAbs(v) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "file://") || looksLikeWindowsAbsolutePath(v) {
			return "[redacted]"
		}
		return v
	default:
		return value
	}
}

func isSensitiveMCPResponseKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	if normalized == "path" || strings.HasSuffix(normalized, "path") ||
		normalized == "dir" || strings.HasSuffix(normalized, "dir") || strings.HasSuffix(normalized, "directory") {
		return true
	}
	if strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential") || strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "apikey") ||
		(strings.Contains(normalized, "key") && (strings.Contains(normalized, "masked") || strings.Contains(normalized, "partial"))) {
		return true
	}
	switch normalized {
	case "args", "arguments", "rawargs", "requestargs", "inputargs",
		"pid", "processid",
		"cdp", "cdpurl", "cdpport", "cdpdefaultport", "debugurl", "debuggerurl", "remotedebuggingport",
		"lease", "leaseowner", "profilelease", "profileleaseowner", "holderid",
		"profile", "profileid", "profiledir", "profilepath", "userdatadir", "browserpath", "executablepath",
		"file", "key", "keymasked", "partialkey", "error", "errormessage", "errordetail", "stderr", "stdout":
		return true
	default:
		return false
	}
}

func looksLikeWindowsAbsolutePath(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `\\`) {
		return true
	}
	return len(value) >= 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z'))
}

// ServeStdio starts the MCP server on stdin/stdout.
func ServeStdio(ctx context.Context, cfg Config) (returnErr error) {
	s, database, err := prepareReadOnlyMCP(cfg)
	if err != nil {
		return err
	}
	defer func() { returnErr = stderrors.Join(returnErr, database.Close()) }()
	return s.ServeStdio(ctx)
}

func prepareReadOnlyMCP(cfg Config) (*Server, *db.DB, error) {
	if cfg.DataDir == "" {
		if v := os.Getenv("ALPHA_NEXUS_DATA_DIR"); v != "" {
			cfg.DataDir = v
		} else {
			home, _ := os.UserHomeDir()
			cfg.DataDir = filepath.Join(home, ".alpha-nexus")
		}
	}
	if cfg.Runtime == nil {
		cfg.Runtime = &tools.Runtime{DataDir: cfg.DataDir}
	} else if cfg.Runtime.DataDir == "" {
		cfg.Runtime.DataDir = cfg.DataDir
	}
	database, err := openReadOnlyDB(cfg.DataDir)
	if err != nil {
		return nil, nil, err
	}
	h := &handlers{db: database, rt: cfg.Runtime, dataDir: cfg.DataDir}
	s := newReadOnlyMCPServer(h, cfg.ExtraRegister)
	s.dataDir = cfg.DataDir
	return s, database, nil
}

func newReadOnlyMCPServer(h *handlers, extraRegister func(*Server)) *Server {
	s := NewServer("alpha-nexus", "1.0.0", agentInstructions)
	registerFriendlyTools(s, h)
	registerRegistryTools(s, h)
	if extraRegister != nil {
		extraRegister(s)
	}
	s.restrictToReadOnly()
	return s
}

func openDB(dataDir string) (*db.DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func openReadOnlyDB(dataDir string) (*db.DB, error) {
	dbPath := filepath.Join(dataDir, "app.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open MCP database read-only: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open MCP database read-only: app.db is not a regular file")
	}
	dsn := &url.URL{Scheme: "file", Path: dbPath}
	query := dsn.Query()
	query.Set("mode", "ro")
	// Immutable mode is required for a strict zero-write projection: SQLite's
	// ordinary mode=ro still mutates WAL shared-memory reader marks.
	walInfo, walErr := os.Lstat(dbPath + "-wal")
	switch {
	case os.IsNotExist(walErr):
	case walErr != nil:
		return nil, fmt.Errorf("open MCP database read-only: inspect WAL: %w", walErr)
	case !walInfo.Mode().IsRegular():
		return nil, fmt.Errorf("open MCP database read-only: app.db-wal is not a regular file")
	}
	query.Set("immutable", "1")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	dsn.RawQuery = query.Encode()
	connection, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open MCP database read-only: %w", err)
	}
	connection.SetMaxOpenConns(1)
	if err := connection.Ping(); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open MCP database read-only: %w", err)
	}
	if err := validateMCPReadSchema(connection); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return &db.DB{DB: connection, DataDir: dataDir}, nil
}

func validateMCPReadSchema(connection *sql.DB) error {
	const requiredTables = 13
	var count int
	if err := connection.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name IN (
			'platforms','accounts','contents','media_assets','publish_records','record_infos',
			'browser_runtime_instances','batch_runs','batch_items','media_library','studio_scripts',
			'generation_jobs','account_login_events'
		)`).Scan(&count); err != nil {
		return stderrors.New("MCP_DATABASE_NOT_INITIALIZED")
	}
	if count != requiredTables {
		return stderrors.New("MCP_DATABASE_NOT_INITIALIZED")
	}
	for _, query := range []string{
		`SELECT avatar_source, avatar_sha256, login_mode, auth_method FROM accounts LIMIT 0`,
		`SELECT mode, references_json FROM generation_jobs LIMIT 0`,
	} {
		rows, err := connection.Query(query)
		if err != nil {
			return stderrors.New("MCP_DATABASE_NOT_INITIALIZED")
		}
		if err := rows.Close(); err != nil {
			return stderrors.New("MCP_DATABASE_NOT_INITIALIZED")
		}
	}
	return nil
}

type handlers struct {
	db      *db.DB
	rt      *tools.Runtime
	dataDir string
}

const agentInstructions = `Alpha-Nexus 本地社媒控制面 MCP（只读）。

可用流程：
1. list_platforms
2. list_accounts / list_logged_accounts
3. list_articles / get_article
4. list_records / get_record_info
5. ego_space_list（仅返回账号绑定的 Space 与接管状态）
6. tools_list / tools_call（仅投影 tools.Registry 中 side_effect=read 的工具）
7. agent_schemes（仅列出方案，不执行）

重要：MCP 不接受模型自报确认，也不执行 SQLite 写入、文件写入、浏览器动作、网络请求或发布。需要副作用的动作只能由可信本地 UI 发起并审批。
`

// ServeStdio runs the JSON-RPC loop with Content-Length framing (MCP standard)
// and also accepts newline-delimited JSON for simple smoke tests.
func (s *Server) ServeStdio(ctx context.Context) error {
	in := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msg, contentLengthFraming, err := readMessage(in)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if len(msg) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			_ = writeMessage(rpcError(nil, -32700, "parse error"), contentLengthFraming)
			continue
		}
		resp := s.handle(ctx, &req)
		if resp == nil {
			continue // notification
		}
		if err := writeMessage(resp, contentLengthFraming); err != nil {
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, req *rpcRequest) *rpcResponse {
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	switch req.Method {
	case "initialize":
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name": s.name, "version": s.version,
				},
				"instructions": s.instructions,
			},
		}
	case "notifications/initialized", "initialized":
		return nil
	case "ping":
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		list := make([]map[string]any, 0, len(s.order))
		for _, name := range s.order {
			t := s.tools[name]
			readOnly := isMCPReadSideEffect(t.SideEffect)
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
				"annotations": map[string]any{
					"readOnlyHint":    readOnly,
					"destructiveHint": !readOnly,
					"idempotentHint":  readOnly,
					"openWorldHint":   t.SideEffect == "network",
				},
			})
		}
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{"tools": list},
		}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcError(req.ID, -32602, err.Error())
		}
		t, ok := s.tools[p.Name]
		if !ok {
			return rpcError(req.ID, -32601, "tool not found")
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		out, err := t.Handler(ctx, p.Arguments)
		text := ""
		isErr := false
		if err != nil {
			isErr = true
			text = publicMCPToolError(err)
		} else {
			safeOut, sanitizeErr := sanitizeMCPOutput(out)
			if sanitizeErr != nil {
				isErr = true
				text = "MCP_RESPONSE_SANITIZATION_FAILED"
			} else {
				switch v := safeOut.(type) {
				case string:
					text = v
				default:
					b, e := json.MarshalIndent(safeOut, "", "  ")
					if e != nil {
						isErr = true
						text = "MCP_RESPONSE_SANITIZATION_FAILED"
					} else {
						text = string(b)
					}
				}
			}
		}
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": text},
				},
				"isError": isErr,
			},
		}
	default:
		if req.ID == nil {
			return nil
		}
		return rpcError(req.ID, -32601, "method not found: "+req.Method)
	}
}

func publicMCPToolError(err error) string {
	if err != nil && strings.Contains(err.Error(), mcpSideEffectDenied) {
		return mcpSideEffectDenied
	}
	return "MCP_TOOL_CALL_FAILED"
}

// ---- framing ----

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id,omitempty"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcError(id any, code int, msg string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcErr{Code: code, Message: msg}}
}

func readMessage(in *bufio.Reader) ([]byte, bool, error) {
	// Peek first non-space
	for {
		b, err := in.ReadByte()
		if err != nil {
			return nil, false, err
		}
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		_ = in.UnreadByte()
		break
	}
	// Content-Length framing?
	peek, err := in.Peek(15)
	if err == nil && strings.HasPrefix(strings.ToLower(string(peek)), "content-length") {
		headers := map[string]string{}
		for {
			line, err := in.ReadString('\n')
			if err != nil {
				return nil, true, err
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
			}
		}
		n, _ := strconv.Atoi(headers["content-length"])
		if n <= 0 {
			return nil, true, fmt.Errorf("invalid Content-Length")
		}
		// hard cap to avoid memory bombs over stdio
		const maxMCPBody = 4 << 20 // 4 MiB
		if n > maxMCPBody {
			return nil, true, fmt.Errorf("Content-Length too large (%d)", n)
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(in, body); err != nil {
			return nil, true, err
		}
		return body, true, nil
	}
	// newline-delimited JSON
	line, err := in.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, false, err
	}
	return []byte(strings.TrimSpace(string(line))), false, nil
}

func writeMessage(v any, contentLengthFraming bool) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if contentLengthFraming {
		header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(b))
		if _, err := os.Stdout.Write([]byte(header)); err != nil {
			return err
		}
		_, err = os.Stdout.Write(b)
		return err
	}
	b = append(b, '\n')
	_, err = os.Stdout.Write(b)
	return err
}

// Schema helpers for tool registration

func schemaObject(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func propString(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func propNumber(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}
func propBool(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func propArray(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
}
func propObject(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc}
}
