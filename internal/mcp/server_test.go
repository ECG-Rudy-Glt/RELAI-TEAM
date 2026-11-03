package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func echoTool() Tool {
	return Tool{Name: "echo", Description: "d", InputSchema: obj(map[string]any{}),
		Handler: func(a json.RawMessage) (string, bool) { return "got " + string(a), false }}
}

// session feeds lines to a server and returns the decoded response lines.
func session(t *testing.T, s *Server, in string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stdout carries a non-JSON line %q: %v", sc.Text(), err)
		}
		res = append(res, m)
	}
	return res
}

func TestInitializeListCallPing(t *testing.T) {
	s := &Server{Name: "x", Version: "1", Instructions: "hi", Tools: []Tool{echoTool()}}
	res := session(t, s, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"a":1}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`,
	}, "\n")+"\n")
	if len(res) != 4 { // the notification gets no answer
		t.Fatalf("got %d responses, want 4: %v", len(res), res)
	}
	init := res[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2024-11-05" || init["instructions"] != "hi" {
		t.Errorf("initialize = %v", init)
	}
	tools := res[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "echo" {
		t.Errorf("tools = %v", tools)
	}
	call := res[2]["result"].(map[string]any)
	if call["isError"] != false || !strings.Contains(call["content"].([]any)[0].(map[string]any)["text"].(string), `"a":1`) {
		t.Errorf("call = %v", call)
	}
}

func TestErrors(t *testing.T) {
	s := &Server{Name: "x", Tools: []Tool{echoTool()}}
	res := session(t, s, strings.Join([]string{
		`not json`,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ghost"}}`,
		`{"jsonrpc":"2.0","method":"notifications/whatever"}`,
	}, "\n")+"\n")
	if len(res) != 3 {
		t.Fatalf("got %d responses: %v", len(res), res)
	}
	for i, code := range []float64{-32700, -32601, -32602} {
		if got := res[i]["error"].(map[string]any)["code"]; got != code {
			t.Errorf("response %d code = %v, want %v", i, got, code)
		}
	}
}

func TestUnknownProtocolVersionFallsBack(t *testing.T) {
	res := session(t, &Server{Name: "x"}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`+"\n")
	if v := res[0]["result"].(map[string]any)["protocolVersion"]; v != supported[0] {
		t.Errorf("version = %v, want %v", v, supported[0])
	}
}
