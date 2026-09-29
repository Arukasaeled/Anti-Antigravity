package patcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type BridgeHandler func(context.Context, map[string]any) (any, error)

type CDPBridge struct {
	conn     net.Conn
	reader   *bufio.Reader
	handlers map[string]BridgeHandler
	ctx      context.Context
	cancel   context.CancelFunc
	writeMu  sync.Mutex
	nextID   int64
}

func StartCDPBridge(ctx context.Context, websocketURL string, handlers map[string]BridgeHandler) (*CDPBridge, error) {
	conn, reader, err := openWebSocket(ctx, websocketURL)
	if err != nil {
		return nil, err
	}
	bridgeCtx, cancel := context.WithCancel(ctx)
	// IDs 1 and 2 are reserved for Runtime.enable/addBinding above. All
	// asynchronous bridge evaluations use a separate range so CDP responses
	// cannot collide with the setup commands.
	bridge := &CDPBridge{conn: conn, reader: reader, handlers: handlers, ctx: bridgeCtx, cancel: cancel, nextID: 1000}
	if err := bridge.command(ctx, 1, "Runtime.enable", nil); err != nil {
		bridge.Close()
		return nil, fmt.Errorf("enable Runtime domain: %w", err)
	}
	if err := bridge.command(ctx, 2, "Runtime.addBinding", map[string]any{"name": "__2AG_IPC__"}); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already") {
		bridge.Close()
		return nil, fmt.Errorf("install __2AG_IPC__ binding: %w", err)
	}
	go bridge.readLoop()
	go func() { <-bridgeCtx.Done(); bridge.Close() }()
	return bridge, nil
}

func (b *CDPBridge) Close() error {
	if b == nil {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}

func (b *CDPBridge) command(ctx context.Context, id int, method string, params any) error {
	request, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	b.writeMu.Lock()
	err = writeWebSocketFrame(b.conn, request)
	b.writeMu.Unlock()
	if err != nil {
		return err
	}
	for {
		frame, opcode, err := readWebSocketFrame(b.reader)
		if err != nil {
			return err
		}
		if opcode == 9 {
			b.writeMu.Lock()
			_ = writeWebSocketControlFrame(b.conn, 10, frame)
			b.writeMu.Unlock()
			continue
		}
		if opcode != 1 {
			continue
		}
		var response struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(frame, &response) != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return errors.New(response.Error.Message)
		}
		return nil
	}
}

func (b *CDPBridge) readLoop() {
	for {
		frame, opcode, err := readWebSocketFrame(b.reader)
		if err != nil {
			return
		}
		if opcode == 9 {
			b.writeMu.Lock()
			_ = writeWebSocketControlFrame(b.conn, 10, frame)
			b.writeMu.Unlock()
			continue
		}
		if opcode != 1 {
			continue
		}
		var commandResult struct {
			ID    *int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				ExceptionDetails json.RawMessage `json:"exceptionDetails"`
			} `json:"result"`
		}
		if json.Unmarshal(frame, &commandResult) == nil && commandResult.ID != nil {
			if commandResult.Error != nil {
				log.Printf("[2ag] CDP IPC command %d failed: %s", *commandResult.ID, commandResult.Error.Message)
			} else if len(commandResult.Result.ExceptionDetails) > 0 && string(commandResult.Result.ExceptionDetails) != "null" {
				log.Printf("[2ag] CDP IPC evaluate exception: %s", string(commandResult.Result.ExceptionDetails))
			}
			continue
		}
		var event struct {
			Method string `json:"method"`
			Params struct {
				Name    string `json:"name"`
				Payload string `json:"payload"`
			} `json:"params"`
		}
		if json.Unmarshal(frame, &event) != nil || event.Method != "Runtime.bindingCalled" || event.Params.Name != "__2AG_IPC__" {
			continue
		}
		go b.dispatch(event.Params.Payload)
	}
}

func (b *CDPBridge) dispatch(payload string) {
	var request struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		b.deliver(map[string]any{"id": request.ID, "error": "invalid IPC request: " + err.Error()})
		return
	}
	response := map[string]any{"id": request.ID}
	if request.Method == "core.diagnostics.devtools" {
		// Prefer the native handler, which can open the browser's DevTools
		// frontend from the loopback CDP endpoint. Keep the DOM key event as a
		// fallback for hosts that already wire their own F12 accelerator.
		if handler := b.handlers[request.Method]; handler != nil {
			result, err := handler(b.ctx, request.Params)
			if err != nil {
				response["error"] = err.Error()
			} else {
				response["result"] = result
			}
			b.deliver(response)
			return
		}
		b.evaluate(`window.__2ag_devtools_dispatching=true;document.dispatchEvent(new KeyboardEvent('keydown',{key:'F12',code:'F12',keyCode:123,which:123,bubbles:true,cancelable:true}));window.__2ag_devtools_dispatching=false;`)
		response["result"] = map[string]any{"dispatched": true}
		b.deliver(response)
		return
	}
	handler := b.handlers[request.Method]
	if handler == nil {
		response["error"] = "unknown IPC method: " + request.Method
		b.deliver(response)
		return
	}
	result, err := handler(b.ctx, request.Params)
	if err != nil {
		response["error"] = err.Error()
	} else {
		response["result"] = result
	}
	b.deliver(response)
}

func (b *CDPBridge) evaluate(expression string) {
	id := int(atomic.AddInt64(&b.nextID, 1))
	request, err := json.Marshal(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "returnByValue": true}})
	if err != nil {
		return
	}
	b.writeMu.Lock()
	if err := writeWebSocketFrame(b.conn, request); err != nil {
		log.Printf("[2ag] CDP IPC response write failed: %v", err)
	}
	b.writeMu.Unlock()
}

func (b *CDPBridge) deliver(response map[string]any) {
	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("[2ag] CDP IPC response encode failed: %v", err)
		return
	}
	payloadLiteral := strconv.Quote(string(data))
	expression := "window.__2AG_IPC_DELIVER__ && window.__2AG_IPC_DELIVER__(" + payloadLiteral + ");"
	id := int(atomic.AddInt64(&b.nextID, 1))
	request, err := json.Marshal(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "returnByValue": true}})
	if err != nil {
		return
	}
	b.writeMu.Lock()
	if err := writeWebSocketFrame(b.conn, request); err != nil {
		log.Printf("[2ag] CDP IPC response write failed: %v", err)
	}
	b.writeMu.Unlock()
}
