package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef uintptr_t EasyMatrixHandle;
typedef uintptr_t EasyMatrixRealtimeHandle;
typedef void (*EasyMatrixRealtimeCallback)(uintptr_t realtime_handle, const char *payload);

static inline void easymatrix_call_realtime(EasyMatrixRealtimeCallback cb, uintptr_t realtime_handle, const char *payload) {
	cb(realtime_handle, payload);
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/cgo"
	"sync"
	"unsafe"

	"github.com/batuhan/easymatrix/internal/embedded"
	"github.com/batuhan/easymatrix/internal/server"
)

type ffiRuntime struct {
	runtime *embedded.Runtime

	mu             sync.Mutex
	nextRealtimeID uintptr
	realtime       map[uintptr]*ffiRealtime
}

type ffiRealtime struct {
	conn *server.EmbeddedRealtimeConnection
}

func cString(value string) *C.char {
	return C.CString(value)
}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}

func marshalResult(value any) *C.char {
	raw, err := json.Marshal(value)
	if err != nil {
		return cString(`{"error":"failed to marshal ffi result"}`)
	}
	return cString(string(raw))
}

func runtimeFromHandle(handle C.EasyMatrixHandle) *ffiRuntime {
	return cgo.Handle(handle).Value().(*ffiRuntime)
}

func invokeJSON(rt *ffiRuntime, payload string) *C.char {
	var cmd embedded.Command
	if err := json.Unmarshal([]byte(payload), &cmd); err != nil {
		return marshalResult(map[string]any{"error": fmt.Sprintf("invalid command: %v", err)})
	}
	result, err := rt.runtime.Execute(context.Background(), cmd)
	if err != nil {
		return marshalResult(map[string]any{"error": err.Error()})
	}
	return marshalResult(result)
}

var (
	lastErrMu sync.Mutex
	lastErr   string
)

func setLastError(err error) {
	lastErrMu.Lock()
	defer lastErrMu.Unlock()
	if err == nil {
		lastErr = ""
	} else {
		lastErr = err.Error()
	}
}

//export EasyMatrixLastError
func EasyMatrixLastError() *C.char {
	lastErrMu.Lock()
	defer lastErrMu.Unlock()
	return cString(lastErr)
}

//export EasyMatrixCreate
func EasyMatrixCreate(cfgJSON *C.char) C.EasyMatrixHandle {
	var cfg embedded.Config
	if raw := goString(cfgJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			setLastError(fmt.Errorf("invalid config json: %w", err))
			return 0
		}
	}
	if cfg.StateDir != "" {
		_ = os.Setenv("GOMUKS_ROOT", cfg.StateDir)
		_ = os.Setenv("DEBUG_DIR", filepath.Join(cfg.StateDir, "logs"))
		_ = os.Setenv("GOMUKS_LOGS_HOME", filepath.Join(cfg.StateDir, "logs"))
		_ = os.Chdir(cfg.StateDir)
	}
	rt, err := embedded.New(cfg)
	if err != nil {
		setLastError(fmt.Errorf("embedded.New: %w", err))
		return 0
	}
	return C.EasyMatrixHandle(cgo.NewHandle(&ffiRuntime{
		runtime:  rt,
		realtime: make(map[uintptr]*ffiRealtime),
	}))
}

//export EasyMatrixStart
func EasyMatrixStart(handle C.EasyMatrixHandle) C.int {
	rt := runtimeFromHandle(handle)
	if err := rt.runtime.Start(context.Background()); err != nil {
		return 1
	}
	return 0
}

//export EasyMatrixServe
func EasyMatrixServe(handle C.EasyMatrixHandle, listenAddr *C.char) C.int {
	rt := runtimeFromHandle(handle)
	if err := rt.runtime.Serve(context.Background(), goString(listenAddr)); err != nil {
		setLastError(fmt.Errorf("serve: %w", err))
		return 1
	}
	return 0
}

//export EasyMatrixResume
func EasyMatrixResume(handle C.EasyMatrixHandle, listenAddr *C.char) C.int {
	rt := runtimeFromHandle(handle)
	if err := rt.runtime.Rebind(context.Background(), goString(listenAddr)); err != nil {
		setLastError(fmt.Errorf("resume: %w", err))
		return 1
	}
	return 0
}

//export EasyMatrixStop
func EasyMatrixStop(handle C.EasyMatrixHandle) {
	runtimeFromHandle(handle).runtime.Stop()
}

//export EasyMatrixDestroy
func EasyMatrixDestroy(handle C.EasyMatrixHandle) {
	h := cgo.Handle(handle)
	rt := h.Value().(*ffiRuntime)
	rt.runtime.Stop()
	rt.mu.Lock()
	for id, conn := range rt.realtime {
		if conn != nil && conn.conn != nil {
			conn.conn.Close()
		}
		delete(rt.realtime, id)
	}
	rt.mu.Unlock()
	h.Delete()
}

//export EasyMatrixHandleRequest
func EasyMatrixHandleRequest(handle C.EasyMatrixHandle, reqJSON *C.char) *C.char {
	var req embedded.Request
	if err := json.Unmarshal([]byte(goString(reqJSON)), &req); err != nil {
		return marshalResult(map[string]any{"error": fmt.Sprintf("invalid request: %v", err)})
	}
	return invokeJSON(runtimeFromHandle(handle), string(mustJSONMarshal(embedded.Command{
		Type:    embedded.CommandTypeHTTPRequest,
		Request: &req,
	})))
}

func mustJSONMarshal(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

//export EasyMatrixInvoke
func EasyMatrixInvoke(handle C.EasyMatrixHandle, cmdJSON *C.char) *C.char {
	return invokeJSON(runtimeFromHandle(handle), goString(cmdJSON))
}

//export EasyMatrixRealtimeConnect
func EasyMatrixRealtimeConnect(handle C.EasyMatrixHandle, callback C.EasyMatrixRealtimeCallback) C.EasyMatrixRealtimeHandle {
	rt := runtimeFromHandle(handle)
	rt.mu.Lock()
	rt.nextRealtimeID++
	realtimeID := rt.nextRealtimeID
	rt.mu.Unlock()

	conn, err := rt.runtime.OpenRealtime(func(payload json.RawMessage) error {
		payloadCString := cString(string(payload))
		C.easymatrix_call_realtime(callback, C.uintptr_t(realtimeID), payloadCString)
		C.free(unsafe.Pointer(payloadCString))
		return nil
	})
	if err != nil {
		return 0
	}

	rt.mu.Lock()
	rt.realtime[realtimeID] = &ffiRealtime{conn: conn}
	rt.mu.Unlock()
	return C.EasyMatrixRealtimeHandle(realtimeID)
}

//export EasyMatrixRealtimeSend
func EasyMatrixRealtimeSend(handle C.EasyMatrixHandle, realtimeHandle C.EasyMatrixRealtimeHandle, payloadJSON *C.char) *C.char {
	rt := runtimeFromHandle(handle)
	rt.mu.Lock()
	conn := rt.realtime[uintptr(realtimeHandle)]
	rt.mu.Unlock()
	if conn == nil || conn.conn == nil {
		return marshalResult(map[string]any{"error": "realtime connection not found"})
	}
	if err := conn.conn.Send([]byte(goString(payloadJSON))); err != nil {
		return marshalResult(map[string]any{"error": err.Error()})
	}
	return nil
}

//export EasyMatrixRealtimeClose
func EasyMatrixRealtimeClose(handle C.EasyMatrixHandle, realtimeHandle C.EasyMatrixRealtimeHandle) {
	rt := runtimeFromHandle(handle)
	rt.mu.Lock()
	conn := rt.realtime[uintptr(realtimeHandle)]
	delete(rt.realtime, uintptr(realtimeHandle))
	rt.mu.Unlock()
	if conn != nil && conn.conn != nil {
		conn.conn.Close()
	}
}

//export EasyMatrixFreeCString
func EasyMatrixFreeCString(value *C.char) {
	if value != nil {
		C.free(unsafe.Pointer(value))
	}
}

func main() {}
