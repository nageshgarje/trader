package mcp

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func ServeHTTP(address string, controller Controller, logger *log.Logger) error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var rpc Request
		if err := json.NewDecoder(http.MaxBytesReader(w, request.Body, 16<<20)).Decode(&rpc); err != nil {
			writeHTTPResponse(w, Response{JSONRPC: JSONRPCVersion, Error: &RPCError{Code: -32700, Message: "Parse error"}})
			return
		}
		response, err := controller.Handle(rpc)
		if err != nil {
			response = Response{JSONRPC: JSONRPCVersion, ID: rpc.ID, Error: &RPCError{Code: -32000, Message: err.Error()}}
		}
		writeHTTPResponse(w, response)
	})
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	if logger != nil {
		logger.Printf("Indian Stock Exchange MCP HTTP server: http://%s/jsonrpc", address)
	}
	return server.ListenAndServe()
}

func writeHTTPResponse(w http.ResponseWriter, response Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
