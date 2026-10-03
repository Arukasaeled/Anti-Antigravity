//go:build windows

package supervisor

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Windows' SQLite API avoids CGo or shipping another database engine. Open with
// SQLITE_OPEN_READONLY; leave live WAL visible, never use immutable mode.
var usageSQLite = syscall.NewLazyDLL("winsqlite3.dll")

func readNativeUsageRows(path, query string, visit func(int64, []byte) error) error {
	open := usageSQLite.NewProc("sqlite3_open_v2")
	closeDB := usageSQLite.NewProc("sqlite3_close")
	prepare := usageSQLite.NewProc("sqlite3_prepare_v2")
	finalize := usageSQLite.NewProc("sqlite3_finalize")
	step := usageSQLite.NewProc("sqlite3_step")
	columnInt := usageSQLite.NewProc("sqlite3_column_int64")
	columnBlob := usageSQLite.NewProc("sqlite3_column_blob")
	columnBytes := usageSQLite.NewProc("sqlite3_column_bytes")
	for _, proc := range []*syscall.LazyProc{open, closeDB, prepare, finalize, step, columnInt, columnBlob, columnBytes} {
		if err := proc.Find(); err != nil {
			return fmt.Errorf("native SQLite unavailable: %w", err)
		}
	}
	name, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	var db uintptr
	rc, _, _ := open.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&db)), 1|0x10000, 0)
	runtime.KeepAlive(name)
	if db != 0 {
		defer closeDB.Call(db)
	}
	if rc != 0 {
		return fmt.Errorf("cannot read conversation database (SQLite %d)", rc)
	}
	if timeout := usageSQLite.NewProc("sqlite3_busy_timeout"); timeout.Find() == nil {
		timeout.Call(db, 100)
	}
	sql, err := syscall.BytePtrFromString(query)
	if err != nil {
		return err
	}
	var statement uintptr
	rc, _, _ = prepare.Call(db, uintptr(unsafe.Pointer(sql)), uintptr(len(query)), uintptr(unsafe.Pointer(&statement)), 0)
	runtime.KeepAlive(sql)
	if statement != 0 {
		defer finalize.Call(statement)
	}
	if rc != 0 {
		return fmt.Errorf("unsupported conversation schema (SQLite %d)", rc)
	}
	bytesRead := 0
	for rows := 0; ; rows++ {
		rc, _, _ = step.Call(statement)
		if rc == 101 {
			return nil
		}
		if rc != 100 {
			return fmt.Errorf("conversation read interrupted (SQLite %d)", rc)
		}
		if rows >= 100000 {
			return fmt.Errorf("conversation exceeds generation read limit")
		}
		idx, _, _ := columnInt.Call(statement, 0)
		length, _, _ := columnBytes.Call(statement, 1)
		if length > 16<<20 || bytesRead+int(length) > 128<<20 {
			return fmt.Errorf("conversation metadata exceeds read limit")
		}
		ptr, _, _ := columnBlob.Call(statement, 1)
		var data []byte
		if length > 0 && ptr != 0 {
			data = append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(length))...)
		}
		bytesRead += len(data)
		if err := visit(int64(idx), data); err != nil {
			return err
		}
	}
}
