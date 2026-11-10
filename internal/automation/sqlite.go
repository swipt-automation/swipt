//go:build darwin && cgo

package automation

/*
#cgo LDFLAGS: -lsqlite3

#include <sqlite3.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	char *schema;
	char *table;
	sqlite3_int64 row;
} swipt_change;

typedef struct {
	swipt_change *items;
	size_t count;
	size_t capacity;
} swipt_changes;

static void swipt_record_change(void *context, int operation, const char *schema, const char *table, sqlite3_int64 row) {
	swipt_changes *changes = context;
	if (changes->count == changes->capacity) {
		size_t capacity = changes->capacity == 0 ? 16 : changes->capacity * 2;
		swipt_change *items = realloc(changes->items, capacity * sizeof(swipt_change));
		if (items == NULL) {
			return;
		}
		changes->items = items;
		changes->capacity = capacity;
	}
	changes->items[changes->count++] = (swipt_change){strdup(schema), strdup(table), row};
}

static void swipt_clear_changes(void *context) {
	swipt_changes *changes = context;
	for (size_t index = 0; index < changes->count; index++) {
		free(changes->items[index].schema);
		free(changes->items[index].table);
	}
	changes->count = 0;
}

static void swipt_watch_changes(sqlite3 *db, swipt_changes *changes) {
	sqlite3_update_hook(db, swipt_record_change, changes);
	sqlite3_rollback_hook(db, swipt_clear_changes, changes);
}

static void swipt_free_changes(swipt_changes *changes) {
	swipt_clear_changes(changes);
	free(changes->items);
	free(changes);
}

static int swipt_interrupted(sqlite3 *db) {
	if (__builtin_available(iOS 17.2, macOS 14.2, *)) {
		return sqlite3_is_interrupted(db);
	}
	return 0;
}

static int swipt_wait(void *context, int count) {
	static const int delays[] = {1, 2, 5, 10, 15, 20, 25, 25, 25, 50, 50, 100};
	static const int steps = sizeof(delays) / sizeof(delays[0]);
	int waited = 0;
	for (int index = 0; index < count; index++) {
		waited += delays[index < steps ? index : steps - 1];
	}
	if (waited >= 5000 || swipt_interrupted(context)) {
		return 0;
	}
	sqlite3_sleep(delays[count < steps ? count : steps - 1]);
	return !swipt_interrupted(context);
}

static void swipt_set_wait(sqlite3 *db) {
	sqlite3_busy_handler(db, swipt_wait, db);
}

static int swipt_bind_text(sqlite3_stmt *statement, int index, const char *text, int length) {
	return sqlite3_bind_text(statement, index, length == 0 ? "" : text, length, SQLITE_TRANSIENT);
}

static int swipt_deserialize(sqlite3 *db, const void *data, sqlite3_int64 size) {
	unsigned char *buffer = sqlite3_malloc64(size);
	if (buffer == NULL) {
		return SQLITE_NOMEM;
	}
	memcpy(buffer, data, size);
	return sqlite3_deserialize(db, "main", buffer, size, size, SQLITE_DESERIALIZE_FREEONCLOSE | SQLITE_DESERIALIZE_RESIZEABLE);
}

static int swipt_inside(const char *directory, const char *file) {
	size_t length = strlen(directory);
	if (strncmp(file, directory, length) != 0 || file[length] != '/') {
		return 0;
	}
	for (const char *segment = file + length; *segment != '\0';) {
		segment++;
		size_t size = strcspn(segment, "/");
		if ((size == 1 && segment[0] == '.') || (size == 2 && segment[0] == '.' && segment[1] == '.')) {
			return 0;
		}
		segment += size;
	}
	return 1;
}

static int swipt_authorize(void *directory, int action, const char *file, const char *unused, const char *schema, const char *trigger) {
	if (action != SQLITE_ATTACH || (file != NULL && (file[0] == '\0' || strcmp(file, ":memory:") == 0))) {
		return SQLITE_OK;
	}
	return file != NULL && sqlite3_strnicmp(file, "file:", 5) != 0 && swipt_inside(directory, file) ? SQLITE_OK : SQLITE_DENY;
}

static void swipt_confine(sqlite3 *db, const char *directory) {
	sqlite3_set_authorizer(db, swipt_authorize, (void *)directory);
}

static int swipt_bind_blob(sqlite3_stmt *statement, int index, const void *data, int length) {
	if (length == 0) {
		return sqlite3_bind_zeroblob(statement, index, 0);
	}
	return sqlite3_bind_blob(statement, index, data, length, SQLITE_TRANSIENT);
}
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"
)

type DatabaseChange struct {
	Swiplet  string
	Database string
	Path     string
	Table    string
	RowID    int64
}

type databaseWatcher struct {
	swiplet string
	publish func(DatabaseChange)
}

type databaseStore struct {
	access    sync.Mutex
	next      int
	databases map[int]*database
	watchers  map[*databaseWatcher]struct{}
}

type database struct {
	access     sync.Mutex
	handle     int
	client     any
	swiplet    string
	directory  string
	path       string
	file       string
	watch      bool
	conn       *C.sqlite3
	confined   *C.char
	changes    *C.swipt_changes
	next       int
	statements map[int]*C.sqlite3_stmt
}

type databaseRequest struct {
	Op           string          `json:"op"`
	Handle       int             `json:"handle"`
	Statement    int             `json:"statement"`
	Path         string          `json:"path"`
	Changes      bool            `json:"changes"`
	SQL          string          `json:"sql"`
	Params       json.RawMessage `json:"params"`
	Count        int             `json:"count"`
	Schema       string          `json:"schema"`
	Source       int             `json:"source"`
	SourceSchema string          `json:"sourceSchema"`
	Data         []byte          `json:"data"`
	Asset        string          `json:"asset"`
	Overwrite    bool            `json:"overwrite"`
}

type databaseRun struct {
	Changes         int64    `json:"changes"`
	LastInsertRowID int64    `json:"lastInsertRowId"`
	Columns         []string `json:"columns"`
	Rows            [][]any  `json:"rows"`
	Done            bool     `json:"done"`
}

type databaseChangeEvent struct {
	DatabaseName     string `json:"databaseName"`
	DatabaseFilePath string `json:"databaseFilePath"`
	TableName        string `json:"tableName"`
	RowID            int64  `json:"rowId"`
}

type databaseChangeBatch struct {
	access sync.Mutex
	taken  bool
	events []databaseChangeEvent
}

type databaseRows struct {
	Rows [][]any `json:"rows"`
	Done bool    `json:"done"`
}

const (
	memoryDatabase = ":memory:"
	statementLimit = 256
	readBudget     = 1 << 20
)

var (
	errDatabaseClosed = E.Cause1(ErrInvalid, E.New("the database is closed"))
	memorySequence    atomic.Uint64
)

func newDatabaseStore() databaseStore {
	return databaseStore{databases: make(map[int]*database), watchers: make(map[*databaseWatcher]struct{})}
}

func sqliteOperation(ctx context.Context, call *operation) (any, error) {
	if len(call.args) != 1 {
		return nil, E.Cause1(ErrInvalid, E.New("takes 1 argument, not ", len(call.args)))
	}
	e := call.engine
	value, err := e.host.SQLite(ctx, e, e.dataOwner(), call.args[0])
	if e.ctx.Err() != nil {
		e.host.CloseDatabases(e, "")
	}
	return value, err
}

func (h *Host) SQLite(ctx context.Context, client any, swiplet string, request []byte) (any, error) {
	var call databaseRequest
	if err := json.Unmarshal(request, &call); err != nil {
		return nil, E.Cause1(ErrInvalid, E.Cause(err, "decode the database request"))
	}
	switch call.Op {
	case "open":
		return h.openDatabase(client, swiplet, call)
	case "deserialize":
		return h.deserializeDatabase(client, swiplet, call)
	case "delete":
		return nil, h.deleteDatabase(swiplet, call.Path)
	case "import":
		return nil, h.importDatabase(swiplet, call)
	case "interrupt":
		h.databases.access.Lock()
		defer h.databases.access.Unlock()
		db, found := h.databases.databases[call.Handle]
		if !found || db.client != client || db.swiplet != swiplet {
			return nil, errDatabaseClosed
		}
		C.sqlite3_interrupt(db.conn)
		return nil, nil
	}
	db, err := h.database(client, swiplet, call.Handle, call.Op == "close")
	if err != nil {
		return nil, err
	}
	switch call.Op {
	case "close":
		db.close()
		return nil, nil
	case "connect":
		return h.connectDatabase(db)
	case "backup":
		source, err := h.database(client, swiplet, call.Source, false)
		if err != nil {
			return nil, err
		}
		return nil, backupDatabase(db, schemaName(call.Schema), source, schemaName(call.SourceSchema))
	}
	db.access.Lock()
	result, err := db.serve(ctx, call)
	changes := db.takeChanges()
	db.access.Unlock()
	h.publishChanges(swiplet, changes)
	return result, err
}

func (h *Host) database(client any, swiplet string, handle int, remove bool) (*database, error) {
	h.databases.access.Lock()
	defer h.databases.access.Unlock()
	db, found := h.databases.databases[handle]
	if !found || db.client != client || db.swiplet != swiplet {
		return nil, errDatabaseClosed
	}
	if remove {
		delete(h.databases.databases, handle)
	}
	return db, nil
}

func schemaName(name string) string {
	if name == "" {
		return "main"
	}
	return name
}

func (h *Host) dataDirectory(swiplet string) (string, error) {
	if swiplet == REPLDirectory {
		return filepath.Join(h.options.Directory, REPLDirectory, DataFolder), nil
	}
	found, err := h.Swiplet(swiplet)
	if err != nil {
		return "", err
	}
	return filepath.Join(found.Directory, DataFolder), nil
}

func (h *Host) databasePath(swiplet, name string) (string, error) {
	if name == memoryDatabase {
		return name, nil
	}
	cleaned, err := cleanName(name)
	if err != nil {
		return "", err
	}
	if cleaned == "." {
		return "", E.Cause1(ErrInvalid, E.New("the database needs a file name"))
	}
	directory, err := h.dataDirectory(swiplet)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, cleaned), nil
}

func (h *Host) openDatabase(client any, swiplet string, call databaseRequest) (any, error) {
	directory, err := h.dataDirectory(swiplet)
	if err != nil {
		return nil, err
	}
	path, err := h.databasePath(swiplet, call.Path)
	if err != nil {
		return nil, err
	}
	file := path
	if path == memoryDatabase {
		file = memoryFile()
	} else if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := openConnection(client, swiplet, directory, path, file, call.Changes)
	if err != nil {
		return nil, E.Cause(err, "open ", strconv.Quote(call.Path))
	}
	return h.register(db), nil
}

func (h *Host) connectDatabase(db *database) (any, error) {
	db.access.Lock()
	defer db.access.Unlock()
	if db.conn == nil {
		return nil, errDatabaseClosed
	}
	connection, err := openConnection(db.client, db.swiplet, db.directory, db.path, db.file, db.watch)
	if err != nil {
		return nil, E.Cause(err, "open a new connection")
	}
	return h.register(connection), nil
}

func (h *Host) deserializeDatabase(client any, swiplet string, call databaseRequest) (any, error) {
	directory, err := h.dataDirectory(swiplet)
	if err != nil {
		return nil, err
	}
	db, err := openConnection(client, swiplet, directory, memoryDatabase, memoryFile(), call.Changes)
	if err != nil {
		return nil, E.Cause(err, "deserialize the database")
	}
	if len(call.Data) > 19 && call.Data[18] == 2 && call.Data[19] == 2 {
		call.Data[18], call.Data[19] = 1, 1
	}
	if len(call.Data) > 0 {
		if err = db.restore(call.Data); err != nil {
			db.close()
			return nil, E.Cause(err, "deserialize the database")
		}
	}
	return h.register(db), nil
}

func memoryFile() string {
	return "file:/swipt-" + strconv.FormatUint(memorySequence.Add(1), 10) + "?vfs=memdb"
}

func openConnection(client any, swiplet, directory, path, file string, watch bool) (*database, error) {
	flags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_NOMUTEX)
	if path == memoryDatabase {
		flags |= C.SQLITE_OPEN_URI
	}
	name := C.CString(file)
	defer C.free(unsafe.Pointer(name))
	var conn *C.sqlite3
	code := C.sqlite3_open_v2(name, &conn, flags, nil)
	db := &database{client: client, swiplet: swiplet, directory: directory, path: path, file: file, watch: watch, conn: conn, statements: make(map[int]*C.sqlite3_stmt)}
	if code != C.SQLITE_OK {
		err := sqliteFailure(conn, code)
		db.close()
		return nil, err
	}
	db.confined = C.CString(directory)
	C.swipt_confine(conn, db.confined)
	C.swipt_set_wait(conn)
	if path != memoryDatabase {
		if err := db.exec("PRAGMA journal_mode = WAL"); err != nil {
			db.close()
			return nil, err
		}
	}
	if watch {
		db.changes = (*C.swipt_changes)(C.calloc(1, C.sizeof_swipt_changes))
		C.swipt_watch_changes(conn, db.changes)
	}
	return db, nil
}

func (h *Host) register(db *database) any {
	h.databases.access.Lock()
	h.databases.next++
	db.handle = h.databases.next
	h.databases.databases[db.handle] = db
	h.databases.access.Unlock()
	return map[string]any{"handle": db.handle, "path": db.path}
}

func (db *database) restore(data []byte) error {
	name := C.CString(memoryDatabase)
	defer C.free(unsafe.Pointer(name))
	var scratch *C.sqlite3
	code := C.sqlite3_open_v2(name, &scratch, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_NOMUTEX, nil)
	defer C.sqlite3_close_v2(scratch)
	if code != C.SQLITE_OK {
		return sqliteFailure(scratch, code)
	}
	if code = C.swipt_deserialize(scratch, unsafe.Pointer(unsafe.SliceData(data)), C.sqlite3_int64(len(data))); code != C.SQLITE_OK {
		return sqliteFailure(scratch, code)
	}
	return copyDatabase(db.conn, "main", scratch, "main")
}

func backupDatabase(dest *database, destSchema string, source *database, sourceSchema string) error {
	first, second := dest, source
	if second.handle < first.handle {
		first, second = second, first
	}
	first.access.Lock()
	defer first.access.Unlock()
	if second != first {
		second.access.Lock()
		defer second.access.Unlock()
	}
	if dest.conn == nil || source.conn == nil {
		return errDatabaseClosed
	}
	return copyDatabase(dest.conn, destSchema, source.conn, sourceSchema)
}

func copyDatabase(dest *C.sqlite3, destSchema string, source *C.sqlite3, sourceSchema string) error {
	destName := C.CString(destSchema)
	defer C.free(unsafe.Pointer(destName))
	sourceName := C.CString(sourceSchema)
	defer C.free(unsafe.Pointer(sourceName))
	backup := C.sqlite3_backup_init(dest, destName, source, sourceName)
	if backup == nil {
		return sqliteFailure(dest, C.sqlite3_errcode(dest))
	}
	code := C.sqlite3_backup_step(backup, -1)
	if finished := C.sqlite3_backup_finish(backup); finished != C.SQLITE_OK {
		return sqliteFailure(dest, finished)
	}
	if code != C.SQLITE_DONE {
		return sqliteFailure(nil, code)
	}
	return nil
}

func (h *Host) deleteDatabase(swiplet, name string) error {
	if name == memoryDatabase {
		return E.Cause1(ErrInvalid, E.New("a memory database cannot be deleted"))
	}
	path, err := h.databasePath(swiplet, name)
	if err != nil {
		return err
	}
	h.databases.access.Lock()
	for _, db := range h.databases.databases {
		if db.path == path {
			h.databases.access.Unlock()
			return E.Cause1(ErrInvalid, E.New("the database ", strconv.Quote(name), " is open"))
		}
	}
	h.databases.access.Unlock()
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if removeErr := os.Remove(path + suffix); !errors.Is(removeErr, os.ErrNotExist) {
			err = E.Errors(err, removeErr)
		}
	}
	return err
}

func (h *Host) importDatabase(swiplet string, call databaseRequest) error {
	if call.Path == memoryDatabase {
		return E.Cause1(ErrInvalid, E.New("a memory database cannot be imported"))
	}
	path, err := h.databasePath(swiplet, call.Path)
	if err != nil {
		return err
	}
	source, err := h.assetPath(swiplet, call.Asset)
	if err != nil {
		return err
	}
	if _, err = os.Stat(path); err == nil && !call.Overwrite {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	staging, err := os.CreateTemp(filepath.Dir(path), ".import-")
	if err != nil {
		return err
	}
	defer os.Remove(staging.Name())
	err = copyFile(staging, source)
	if closeErr := staging.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return E.Cause(err, "copy ", strconv.Quote(call.Asset))
	}
	if err = h.deleteDatabase(swiplet, call.Path); err != nil {
		return err
	}
	return os.Rename(staging.Name(), path)
}

func (h *Host) assetPath(swiplet, asset string) (string, error) {
	found, err := h.Swiplet(swiplet)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(found.Directory, AssetsFolder)
	if !strings.HasPrefix(asset, "file://") {
		cleaned, err := cleanName(asset)
		if err != nil {
			return "", err
		}
		return filepath.Join(directory, cleaned), nil
	}
	location, err := url.Parse(asset)
	if err != nil {
		return "", E.Cause1(ErrInvalid, E.Cause(err, "parse ", strconv.Quote(asset)))
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return "", E.Cause1(ErrNotFound, E.New("the swiplet has no assets"))
	}
	resolved, err := filepath.EvalSymlinks(location.Path)
	if err != nil {
		return "", E.Cause1(ErrNotFound, E.New("there is no asset ", strconv.Quote(asset)))
	}
	if relative, err := filepath.Rel(directory, resolved); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", E.Cause1(ErrNotAllowed, E.New(strconv.Quote(asset), " is not an asset of the swiplet"))
	}
	return resolved, nil
}

func copyFile(target *os.File, source string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(target, file)
	return err
}

func (h *Host) watchDatabaseChanges(ctx context.Context, swiplet string, publish func(any)) {
	var access sync.Mutex
	var batch *databaseChangeBatch
	h.WatchDatabases(ctx, swiplet, func(change DatabaseChange) {
		event := databaseChangeEvent{DatabaseName: change.Database, DatabaseFilePath: change.Path, TableName: change.Table, RowID: change.RowID}
		access.Lock()
		defer access.Unlock()
		if batch != nil && batch.add(event) {
			return
		}
		batch = &databaseChangeBatch{events: []databaseChangeEvent{event}}
		publish(batch)
	})
}

func (b *databaseChangeBatch) add(event databaseChangeEvent) bool {
	b.access.Lock()
	defer b.access.Unlock()
	if !b.taken {
		b.events = append(b.events, event)
	}
	return !b.taken
}

func (b *databaseChangeBatch) MarshalJSON() ([]byte, error) {
	b.access.Lock()
	b.taken = true
	events := b.events
	b.events = nil
	b.access.Unlock()
	return json.Marshal(events)
}

func (h *Host) CloseDatabases(client any, swiplet string) {
	h.databases.access.Lock()
	var closing []*database
	for handle, db := range h.databases.databases {
		if (client == nil || db.client == client) && (swiplet == "" || db.swiplet == swiplet) {
			C.sqlite3_interrupt(db.conn)
			closing = append(closing, db)
			delete(h.databases.databases, handle)
		}
	}
	h.databases.access.Unlock()
	for _, db := range closing {
		db.close()
	}
}

func (h *Host) WatchDatabases(ctx context.Context, swiplet string, publish func(DatabaseChange)) {
	watcher := &databaseWatcher{swiplet: swiplet, publish: publish}
	h.databases.access.Lock()
	h.databases.watchers[watcher] = struct{}{}
	h.databases.access.Unlock()
	<-ctx.Done()
	h.databases.access.Lock()
	delete(h.databases.watchers, watcher)
	h.databases.access.Unlock()
}

func (h *Host) publishChanges(swiplet string, changes []DatabaseChange) {
	if len(changes) == 0 {
		return
	}
	h.databases.access.Lock()
	var watchers []*databaseWatcher
	for watcher := range h.databases.watchers {
		if watcher.swiplet == "" || watcher.swiplet == swiplet {
			watchers = append(watchers, watcher)
		}
	}
	h.databases.access.Unlock()
	for _, watcher := range watchers {
		for _, change := range changes {
			watcher.publish(change)
		}
	}
}

func (db *database) close() {
	db.access.Lock()
	defer db.access.Unlock()
	for _, stmt := range db.statements {
		C.sqlite3_finalize(stmt)
	}
	clear(db.statements)
	C.sqlite3_close_v2(db.conn)
	db.conn = nil
	if db.confined != nil {
		C.free(unsafe.Pointer(db.confined))
		db.confined = nil
	}
	if db.changes != nil {
		C.swipt_free_changes(db.changes)
		db.changes = nil
	}
}

func sqliteFailure(conn *C.sqlite3, code C.int) error {
	message := C.GoString(C.sqlite3_errstr(code))
	if conn != nil && C.sqlite3_errcode(conn) != C.SQLITE_OK {
		message = C.GoString(C.sqlite3_errmsg(conn))
	}
	return E.Cause1(ErrInvalid, E.New(message))
}

func (db *database) serve(ctx context.Context, call databaseRequest) (any, error) {
	if db.conn == nil {
		return nil, errDatabaseClosed
	}
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	conn := db.conn
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		C.sqlite3_interrupt(conn)
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	switch call.Op {
	case "exec":
		return nil, db.exec(call.SQL)
	case "prepare":
		return db.prepare(call.SQL)
	case "query":
		stmt, err := db.compile(call.SQL, 0)
		if err != nil {
			return nil, err
		}
		defer C.sqlite3_finalize(stmt)
		return db.run(stmt, call.Params, call.Count)
	case "finalize":
		if stmt, found := db.statements[call.Statement]; found {
			delete(db.statements, call.Statement)
			C.sqlite3_finalize(stmt)
		}
		return nil, nil
	case "run", "step", "reset":
		stmt, found := db.statements[call.Statement]
		if !found {
			return nil, E.Cause1(ErrInvalid, E.New("the statement is finalized"))
		}
		switch call.Op {
		case "run":
			return db.run(stmt, call.Params, call.Count)
		case "step":
			rows, done, err := db.read(stmt, call.Count)
			if err != nil {
				return nil, err
			}
			return databaseRows{Rows: rows, Done: done}, nil
		}
		C.sqlite3_reset(stmt)
		return nil, nil
	case "autocommit":
		return C.sqlite3_get_autocommit(db.conn) != 0, nil
	case "serialize":
		name := C.CString(schemaName(call.Schema))
		defer C.free(unsafe.Pointer(name))
		var size C.sqlite3_int64
		data := C.sqlite3_serialize(db.conn, name, &size, 0)
		if data == nil {
			return nil, E.Cause1(ErrInvalid, E.New("the database ", strconv.Quote(schemaName(call.Schema)), " cannot be serialized"))
		}
		defer C.sqlite3_free(unsafe.Pointer(data))
		return map[string][]byte{"data": bytes.Clone(unsafe.Slice((*byte)(unsafe.Pointer(data)), int(size)))}, nil
	}
	return nil, E.Cause1(ErrInvalid, E.New("unknown database operation ", strconv.Quote(call.Op)))
}

func (db *database) exec(source string) error {
	text := C.CString(source)
	defer C.free(unsafe.Pointer(text))
	if code := C.sqlite3_exec(db.conn, text, nil, nil, nil); code != C.SQLITE_OK {
		return sqliteFailure(db.conn, code)
	}
	return nil
}

func (db *database) compile(source string, flags C.uint) (*C.sqlite3_stmt, error) {
	if source == "" {
		return nil, E.Cause1(ErrInvalid, E.New("the SQL has no statement"))
	}
	var stmt *C.sqlite3_stmt
	code := C.sqlite3_prepare_v3(db.conn, (*C.char)(unsafe.Pointer(unsafe.StringData(source))), C.int(len(source)), flags, &stmt, nil)
	if code != C.SQLITE_OK {
		return nil, sqliteFailure(db.conn, code)
	}
	if stmt == nil {
		return nil, E.Cause1(ErrInvalid, E.New("the SQL has no statement"))
	}
	return stmt, nil
}

func (db *database) prepare(source string) (any, error) {
	stmt, err := db.compile(source, C.SQLITE_PREPARE_PERSISTENT)
	if err != nil {
		return nil, err
	}
	if len(db.statements) >= statementLimit {
		handles := slices.Sorted(maps.Keys(db.statements))
		evicted := handles[0]
		if index := slices.IndexFunc(handles, func(handle int) bool { return C.sqlite3_stmt_busy(db.statements[handle]) == 0 }); index >= 0 {
			evicted = handles[index]
		}
		C.sqlite3_finalize(db.statements[evicted])
		delete(db.statements, evicted)
	}
	db.next++
	db.statements[db.next] = stmt
	return map[string]any{"statement": db.next, "columns": columnNames(stmt)}, nil
}

func (db *database) run(stmt *C.sqlite3_stmt, params json.RawMessage, count int) (any, error) {
	C.sqlite3_reset(stmt)
	C.sqlite3_clear_bindings(stmt)
	if err := db.bind(stmt, params); err != nil {
		return nil, err
	}
	rows, done, err := db.read(stmt, count)
	if err != nil {
		return nil, err
	}
	return databaseRun{
		Changes:         int64(C.sqlite3_changes64(db.conn)),
		LastInsertRowID: int64(C.sqlite3_last_insert_rowid(db.conn)),
		Columns:         columnNames(stmt),
		Rows:            rows,
		Done:            done,
	}, nil
}

func (db *database) read(stmt *C.sqlite3_stmt, count int) ([][]any, bool, error) {
	rows := [][]any{}
	size := 0
	for count == 0 || (len(rows) < count && size < readBudget) {
		code := C.sqlite3_step(stmt)
		if code == C.SQLITE_DONE {
			C.sqlite3_reset(stmt)
			return rows, true, nil
		}
		if code != C.SQLITE_ROW {
			err := sqliteFailure(db.conn, code)
			C.sqlite3_reset(stmt)
			return nil, true, err
		}
		row := make([]any, int(C.sqlite3_column_count(stmt)))
		for index := range row {
			row[index] = columnValue(stmt, C.int(index))
			size += int(C.sqlite3_column_bytes(stmt, C.int(index)))
		}
		rows = append(rows, row)
	}
	return rows, false, nil
}

func (db *database) bind(stmt *C.sqlite3_stmt, encoded json.RawMessage) error {
	if len(encoded) == 0 || string(encoded) == "null" {
		return nil
	}
	var list []json.RawMessage
	if json.Unmarshal(encoded, &list) == nil {
		for index, item := range list {
			if err := db.bindValue(stmt, C.int(index+1), item); err != nil {
				return err
			}
		}
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return E.Cause1(ErrInvalid, E.New("the parameters must be an array or an object"))
	}
	for key, item := range object {
		index := parameterIndex(stmt, key)
		if index == 0 {
			continue
		}
		if err := db.bindValue(stmt, index, item); err != nil {
			return err
		}
	}
	return nil
}

func parameterIndex(stmt *C.sqlite3_stmt, key string) C.int {
	names := []string{key}
	if key == "" || !strings.ContainsRune("$:@?", rune(key[0])) {
		names = []string{"$" + key, ":" + key, "@" + key}
	}
	for _, name := range names {
		text := C.CString(name)
		index := C.sqlite3_bind_parameter_index(stmt, text)
		C.free(unsafe.Pointer(text))
		if index != 0 {
			return index
		}
	}
	return 0
}

func (db *database) bindValue(stmt *C.sqlite3_stmt, index C.int, item json.RawMessage) error {
	decoder := json.NewDecoder(strings.NewReader(string(item)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return E.Cause1(ErrInvalid, E.Cause(err, "decode a parameter"))
	}
	var code C.int
	switch v := value.(type) {
	case nil:
		code = C.sqlite3_bind_null(stmt, index)
	case bool:
		var integer C.sqlite3_int64
		if v {
			integer = 1
		}
		code = C.sqlite3_bind_int64(stmt, index, integer)
	case string:
		code = C.swipt_bind_text(stmt, index, (*C.char)(unsafe.Pointer(unsafe.StringData(v))), C.int(len(v)))
	case json.Number:
		if integer, err := v.Int64(); err == nil {
			code = C.sqlite3_bind_int64(stmt, index, C.sqlite3_int64(integer))
		} else if float, err := v.Float64(); err == nil {
			code = C.sqlite3_bind_double(stmt, index, C.double(float))
		} else {
			return E.Cause1(ErrInvalid, E.New("the number ", v.String(), " is out of range"))
		}
	case map[string]any:
		if encoded, ok := v["$b"].(string); ok && len(v) == 1 {
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return E.Cause1(ErrInvalid, E.Cause(err, "decode a blob parameter"))
			}
			code = C.swipt_bind_blob(stmt, index, unsafe.Pointer(unsafe.SliceData(data)), C.int(len(data)))
		} else if decimal, ok := v["$i"].(string); ok && len(v) == 1 {
			integer, err := strconv.ParseInt(decimal, 10, 64)
			if err != nil {
				return E.Cause1(ErrInvalid, E.New("the bigint ", decimal, " is out of the 64-bit range"))
			}
			code = C.sqlite3_bind_int64(stmt, index, C.sqlite3_int64(integer))
		} else {
			return E.Cause1(ErrInvalid, E.New("a parameter must be null, a number, a bigint, a string, a boolean or a Uint8Array"))
		}
	default:
		return E.Cause1(ErrInvalid, E.New("a parameter must be null, a number, a bigint, a string, a boolean or a Uint8Array"))
	}
	if code != C.SQLITE_OK {
		return sqliteFailure(db.conn, code)
	}
	return nil
}

func (db *database) takeChanges() []DatabaseChange {
	if db.changes == nil || db.changes.count == 0 || C.sqlite3_txn_state(db.conn, nil) == C.SQLITE_TXN_WRITE {
		return nil
	}
	items := unsafe.Slice(db.changes.items, db.changes.count)
	changes := make([]DatabaseChange, len(items))
	for index, item := range items {
		changes[index] = DatabaseChange{Swiplet: db.swiplet, Database: C.GoString(item.schema), Path: db.path, Table: C.GoString(item.table), RowID: int64(item.row)}
	}
	C.swipt_clear_changes(unsafe.Pointer(db.changes))
	return changes
}

func columnNames(stmt *C.sqlite3_stmt) []string {
	names := make([]string, int(C.sqlite3_column_count(stmt)))
	for index := range names {
		names[index] = C.GoString(C.sqlite3_column_name(stmt, C.int(index)))
	}
	return names
}

func columnValue(stmt *C.sqlite3_stmt, index C.int) any {
	switch C.sqlite3_column_type(stmt, index) {
	case C.SQLITE_INTEGER:
		return int64(C.sqlite3_column_int64(stmt, index))
	case C.SQLITE_FLOAT:
		value := float64(C.sqlite3_column_double(stmt, index))
		if math.IsInf(value, 0) {
			return nil
		}
		return value
	case C.SQLITE_TEXT:
		text := C.sqlite3_column_text(stmt, index)
		return C.GoStringN((*C.char)(unsafe.Pointer(text)), C.sqlite3_column_bytes(stmt, index))
	case C.SQLITE_BLOB:
		data := C.sqlite3_column_blob(stmt, index)
		return map[string]string{"$b": base64.StdEncoding.EncodeToString(C.GoBytes(data, C.sqlite3_column_bytes(stmt, index)))}
	}
	return nil
}
