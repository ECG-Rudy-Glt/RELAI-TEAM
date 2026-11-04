// Package mcp is a minimal Model Context Protocol server over stdio: newline-delimited JSON-RPC
// 2.0, tools only. It is just enough for agents to call the team board; see the mcp-team-interface
// change for why there is no SDK.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Tool is one callable tool. Handler returns the text for the agent and whether it is an error.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(args json.RawMessage) (text string, isError bool)
}

// Server serves tools over a reader/writer pair.
type Server struct {
	Name         string
	Version      string
	Instructions string
	Tools        []Tool
}

// protocol versions we know; the client's is echoed back when it is one of these.
var supported = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from in until EOF and writes responses to out. Only protocol messages are
// ever written to out; anything else would corrupt the stream, so diagnostics must go elsewhere.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	write := func(r response) {
		r.JSONRPC = "2.0"
		b, _ := json.Marshal(r)
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(out, "%s\n", b)
	}
	br := bufio.NewReaderSize(in, 64*1024)
	for {
		line, tooLong, err := readLine(br, maxLine)
		if tooLong {
			// Answer and keep serving: one oversized request must not end the whole session.
			write(response{ID: json.RawMessage("null"), Error: &rpcError{-32600, "request larger than 4 MiB, ignored"}})
		}
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			write(response{ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		isNotification := len(req.ID) == 0
		result, rerr := s.handle(req)
		if isNotification {
			continue
		}
		write(response{ID: req.ID, Result: result, Error: rerr})
	}
}

// maxLine is the largest request line accepted.
const maxLine = 4 * 1024 * 1024

// readLine returns the next line without its newline. A line longer than max is skipped to its end
// and reported with tooLong, instead of failing the stream the way bufio.Scanner does. At EOF it
// returns what is left (a last line with no newline) and io.EOF on the call after.
func readLine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, rerr := r.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > max+1 {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		switch rerr {
		case nil:
			return bytes.TrimRight(buf, "\r\n"), tooLong, nil
		case bufio.ErrBufferFull:
			continue
		default:
			return bytes.TrimRight(buf, "\r\n"), tooLong, rerr
		}
	}
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := supported[0]
		for _, v := range supported {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		res := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		list := make([]map[string]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		for _, t := range s.Tools {
			if t.Name == p.Name {
				text, isErr := t.Handler(p.Arguments)
				return map[string]any{
					"content": []map[string]string{{"type": "text", "text": text}},
					"isError": isErr,
				}, nil
			}
		}
		return nil, &rpcError{-32602, "unknown tool: " + p.Name}
	}
	if len(req.ID) == 0 { // unknown notification: ignore
		return nil, nil
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}
