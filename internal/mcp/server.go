package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"strings"
)

const JSONRPCVersion = "2.0"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Serve(input io.Reader, output io.Writer, controller Controller, logger *log.Logger) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var request Request
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			if err := encoder.Encode(Response{JSONRPC: JSONRPCVersion, Error: &RPCError{Code: -32700, Message: "Parse error"}}); err != nil {
				return err
			}
			continue
		}
		response, err := controller.Handle(request)
		if err != nil {
			response = Response{JSONRPC: JSONRPCVersion, ID: request.ID, Error: &RPCError{Code: -32000, Message: err.Error()}}
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	if logger != nil && scanner.Err() != nil {
		logger.Printf("MCP input error: %v", scanner.Err())
	}
	return scanner.Err()
}
