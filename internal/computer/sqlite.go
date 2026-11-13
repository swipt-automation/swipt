//go:build cgo

package computer

/*
#cgo LDFLAGS: -lsqlite3

#include <sqlite3.h>
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"
)

func sqliteOpen(file string) (*C.sqlite3, error) {
	name := C.CString(file)
	defer C.free(unsafe.Pointer(name))
	var db *C.sqlite3
	if code := C.sqlite3_open_v2(name, &db, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE, nil); code != C.SQLITE_OK {
		err := sqliteError(db, code)
		C.sqlite3_close(db)
		return nil, err
	}
	return db, nil
}

func sqliteError(db *C.sqlite3, code C.int) error {
	if db != nil {
		return E.New("sqlite: ", C.GoString(C.sqlite3_errmsg(db)))
	}
	return E.New("sqlite: ", C.GoString(C.sqlite3_errstr(code)))
}

func sqliteExecute(file, statements string) error {
	db, err := sqliteOpen(file)
	if err != nil {
		return err
	}
	defer C.sqlite3_close(db)
	script := C.CString(statements)
	defer C.free(unsafe.Pointer(script))
	if code := C.sqlite3_exec(db, script, nil, nil, nil); code != C.SQLITE_OK {
		return sqliteError(db, code)
	}
	return nil
}

func sqliteQuery(file, query string) ([]map[string]any, error) {
	db, err := sqliteOpen(file)
	if err != nil {
		return nil, err
	}
	defer C.sqlite3_close(db)
	text := C.CString(query)
	defer C.free(unsafe.Pointer(text))
	var statement *C.sqlite3_stmt
	if code := C.sqlite3_prepare_v2(db, text, -1, &statement, nil); code != C.SQLITE_OK {
		return nil, sqliteError(db, code)
	}
	defer C.sqlite3_finalize(statement)
	rows := []map[string]any{}
	for {
		code := C.sqlite3_step(statement)
		if code == C.SQLITE_DONE {
			return rows, nil
		}
		if code != C.SQLITE_ROW {
			return nil, sqliteError(db, code)
		}
		row := map[string]any{}
		for column := range C.sqlite3_column_count(statement) {
			name := C.GoString(C.sqlite3_column_name(statement, column))
			switch C.sqlite3_column_type(statement, column) {
			case C.SQLITE_INTEGER:
				row[name] = int64(C.sqlite3_column_int64(statement, column))
			case C.SQLITE_FLOAT:
				row[name] = float64(C.sqlite3_column_double(statement, column))
			case C.SQLITE_TEXT:
				row[name] = C.GoStringN((*C.char)(unsafe.Pointer(C.sqlite3_column_text(statement, column))), C.sqlite3_column_bytes(statement, column))
			case C.SQLITE_BLOB:
				row[name] = C.GoBytes(C.sqlite3_column_blob(statement, column), C.sqlite3_column_bytes(statement, column))
			default:
				row[name] = nil
			}
		}
		rows = append(rows, row)
	}
}
