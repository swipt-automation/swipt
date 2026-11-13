import { decodeBase64, encodeBase64 } from "./base64";
import { nameOf } from "./paths";

export interface DatabaseChangeEvent {
  databaseName: string;
  databaseFilePath: string;
  tableName: string;
  rowId: number;
}

export interface SQLiteBridge {
  request(request: Record<string, unknown>): unknown;
  requestAsync(request: Record<string, unknown>): Promise<unknown>;
  readonly defaultDirectory: string;
  subscribe(listener: (event: DatabaseChangeEvent) => void): () => void;
}

export type SQLiteBindValue =
  string | number | boolean | bigint | null | Uint8Array;
export type SQLiteBindParams =
  Record<string, SQLiteBindValue> | SQLiteBindValue[];
export type SQLiteVariadicBindParams = SQLiteBindValue[];

export interface SQLiteRunResult {
  lastInsertRowId: number;
  changes: number;
}

export interface SQLiteOpenOptions {
  enableChangeListener?: boolean;
  useNewConnection?: boolean;
  [key: string]: unknown;
}

interface Opened {
  handle: number;
  path: string;
}

interface Prepared {
  statement: number;
  columns: string[];
}

interface Read {
  rows: unknown[][];
  done: boolean;
}

interface Ran extends Read {
  changes: number;
  lastInsertRowId: number;
  columns: string[];
}

export interface SQLiteExecuteSyncResult<T> extends IterableIterator<T> {
  readonly lastInsertRowId: number;
  readonly changes: number;
  getFirstSync(): T | null;
  getAllSync(): T[];
  resetSync(): void;
}

export interface SQLiteExecuteAsyncResult<T> extends AsyncIterableIterator<T> {
  readonly lastInsertRowId: number;
  readonly changes: number;
  getFirstAsync(): Promise<T | null>;
  getAllAsync(): Promise<T[]>;
  resetAsync(): Promise<void>;
}

export interface SQLiteProviderAssetSource {
  assetId: unknown;
  forceOverwrite?: boolean;
}

export type SQLiteStorageSetItemUpdateFunction = (
  previous: string | null,
) => string;

export interface SQLiteBackupOptions {
  sourceDatabase: unknown;
  sourceDatabaseName?: string;
  destDatabase: unknown;
  destDatabaseName?: string;
}

const isPlainObject = (value: unknown): value is Record<string, unknown> =>
  value !== null &&
  typeof value === "object" &&
  !Array.isArray(value) &&
  !(value instanceof Uint8Array);

const encodeValue = (value: unknown): unknown => {
  switch (typeof value) {
    case "undefined":
      return null;
    case "bigint":
      return { $i: value.toString() };
    case "number":
    case "string":
    case "boolean":
      return value;
    case "object":
      if (value === null) {
        return null;
      }
      if (value instanceof Uint8Array) {
        return { $b: encodeBase64(value) };
      }
      if (value instanceof Date) {
        return value.toISOString();
      }
      break;
  }
  throw new TypeError(
    `a parameter must be null, a number, a string, a boolean, a bigint or a Uint8Array, not ${typeof value}`,
  );
};

const encodeParams = (params: unknown[]): unknown => {
  if (params.length === 1 && params[0] == null) {
    return [];
  }
  if (params.length === 1 && Array.isArray(params[0])) {
    return params[0].map(encodeValue);
  }
  if (params.length === 1 && isPlainObject(params[0])) {
    return Object.fromEntries(
      Object.entries(params[0]).map(([key, value]) => [
        key,
        encodeValue(value),
      ]),
    );
  }
  return params.map(encodeValue);
};

const decodeValue = (value: unknown): unknown =>
  isPlainObject(value) && typeof value.$b === "string"
    ? decodeBase64(value.$b)
    : value;

const composer =
  <T>(columns: string[], raw: boolean) =>
  (row: unknown[]): T => {
    if (raw) {
      return row.map(decodeValue) as T;
    }
    const object: Record<string, unknown> = {};
    for (let index = 0; index < columns.length; index++) {
      object[columns[index]] = decodeValue(row[index]);
    }
    return object as T;
  };

const toBytes = (value: unknown, name: string): Uint8Array => {
  if (value instanceof Uint8Array) {
    return value;
  }
  if (value instanceof ArrayBuffer) {
    return new Uint8Array(value);
  }
  if (ArrayBuffer.isView(value)) {
    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  }
  throw new TypeError(`${name} must be a Uint8Array`);
};

export const deepEqual = (a: unknown, b: unknown): boolean => {
  if (a === b) {
    return true;
  }
  if (
    a === null ||
    b === null ||
    typeof a !== "object" ||
    typeof b !== "object"
  ) {
    return false;
  }
  const keys = Object.keys(a);
  return (
    keys.length === Object.keys(b).length &&
    keys.every((key) =>
      deepEqual(
        (a as Record<string, unknown>)[key],
        (b as Record<string, unknown>)[key],
      ),
    )
  );
};

const rowBatch = 128;

const supersededMessage =
  "the result can't be read because its statement ran again; read a result to the end before running its statement again, or prepare a statement for each reader";

const shiftedMessage = (reset: string) =>
  `the cursor has moved past the first row; call ${reset}() before reading the rows again`;

const ignore = () => {};

export function createSQLite(bridge: SQLiteBridge) {
  const connections = new Set<Connection>();
  const owners = new WeakMap<object, Connection>();
  let closed = false;

  const finalizedByHost = (error: unknown) =>
    error instanceof Error &&
    error.message.includes("the statement is finalized");

  type Send = <T>(call: Record<string, unknown>) => Promise<T>;

  class Connection {
    handle: number | null;
    tail: Promise<unknown> = Promise.resolve();

    constructor(handle: number) {
      this.handle = handle;
      connections.add(this);
      if (closed) {
        this.discard();
      }
    }

    get open(): number {
      if (this.handle === null) {
        throw new Error("the database is closed");
      }
      return this.handle;
    }

    call<T>(call: Record<string, unknown>): T {
      return bridge.request({ ...call, handle: this.open }) as T;
    }

    perform<T>(work: (send: Send) => Promise<T>): Promise<T> {
      return schedule([this], () =>
        work(
          (call) =>
            bridge.requestAsync({
              ...call,
              handle: this.open,
            }) as Promise<never>,
        ),
      );
    }

    callAsync<T>(call: Record<string, unknown>): Promise<T> {
      return this.perform((send) => send<T>(call));
    }

    #release(): number | null {
      const handle = this.handle;
      this.handle = null;
      connections.delete(this);
      return handle;
    }

    close(): void {
      const handle = this.#release();
      if (handle !== null) {
        bridge.request({ op: "close", handle });
      }
    }

    closeAsync(): Promise<void> {
      return schedule([this], async () => {
        const handle = this.#release();
        if (handle !== null) {
          await bridge.requestAsync({ op: "close", handle });
        }
      });
    }

    discard(): void {
      const handle = this.#release();
      if (handle !== null) {
        bridge.requestAsync({ op: "close", handle }).catch(ignore);
      }
    }
  }

  const schedule = <T>(
    queue: Connection[],
    task: () => Promise<T>,
  ): Promise<T> => {
    const run = Promise.all(queue.map((connection) => connection.tail)).then(
      task,
    );
    const settled = run.then(ignore, ignore);
    for (const connection of queue) {
      connection.tail = settled;
    }
    return run;
  };

  const connectionOf = (database: unknown, name: string): Connection => {
    const connection =
      typeof database === "object" && database !== null
        ? owners.get(database)
        : undefined;
    if (connection === undefined) {
      throw new TypeError(
        `${name} is not a database that openDatabaseSync or openDatabaseAsync opened`,
      );
    }
    return connection;
  };

  const querySync = (
    connection: Connection,
    source: string,
    params: unknown[],
    count: number,
  ): Ran =>
    connection.call<Ran>({
      op: "query",
      sql: String(source),
      params: encodeParams(params),
      count,
    });

  const queryAsync = (
    connection: Connection,
    source: string,
    params: unknown[],
    count: number,
  ): Promise<Ran> =>
    connection.callAsync<Ran>({
      op: "query",
      sql: String(source),
      params: encodeParams(params),
      count,
    });

  const firstOf = <T>(ran: Ran, raw: boolean): T | null =>
    ran.rows.length === 0 ? null : composer<T>(ran.columns, raw)(ran.rows[0]);

  const allOf = <T>(ran: Ran, raw: boolean): T[] =>
    ran.rows.map(composer<T>(ran.columns, raw));

  const taggedResultOf = <T>(ran: Ran): T[] | SQLiteRunResult =>
    ran.columns.length > 0
      ? allOf<T>(ran, false)
      : { lastInsertRowId: ran.lastInsertRowId, changes: ran.changes };

  class SQLiteStatement {
    #connection: Connection;
    #source: string;
    #id: number | null;
    #columns: string[];
    #cursor: object | null = null;

    constructor(connection: Connection, source: string, prepared: Prepared) {
      this.#connection = connection;
      this.#source = source;
      this.#id = prepared.statement;
      this.#columns = prepared.columns;
    }

    get #live(): number {
      if (this.#id === null) {
        throw new Error("the statement is finalized");
      }
      return this.#id;
    }

    #adopt(prepared: Prepared): number {
      this.#id = prepared.statement;
      this.#columns = prepared.columns;
      return prepared.statement;
    }

    #claim(): () => boolean {
      const cursor = {};
      this.#cursor = cursor;
      return () => this.#cursor === cursor;
    }

    #runSync(params: unknown[]): Ran {
      const call = (statement: number) => ({
        op: "run",
        statement,
        params: encodeParams(params),
        count: 1,
      });
      const statement = this.#live;
      try {
        return this.#connection.call<Ran>(call(statement));
      } catch (error) {
        if (!finalizedByHost(error)) {
          throw error;
        }
        return this.#connection.call<Ran>(
          call(
            this.#adopt(
              this.#connection.call<Prepared>({
                op: "prepare",
                sql: this.#source,
              }),
            ),
          ),
        );
      }
    }

    #runAsync(params: unknown[]): Promise<Ran> {
      const encoded = encodeParams(params);
      const call = (statement: number) => ({
        op: "run",
        statement,
        params: encoded,
        count: 1,
      });
      return this.#connection.perform(async (send) => {
        try {
          return await send<Ran>(call(this.#live));
        } catch (error) {
          if (!finalizedByHost(error) || this.#id === null) {
            throw error;
          }
          return send<Ran>(
            call(
              this.#adopt(
                await send<Prepared>({ op: "prepare", sql: this.#source }),
              ),
            ),
          );
        }
      });
    }

    #syncResult<T>(ran: Ran, raw: boolean): SQLiteExecuteSyncResult<T> {
      const owns = this.#claim();
      const compose = composer<T>(ran.columns, raw);
      let buffered = ran.rows;
      let done = ran.done;
      let stepped = false;
      const pull = (count: number): unknown[][] => {
        const rows = buffered;
        buffered = [];
        if (done || (count !== 0 && rows.length > 0)) {
          return rows;
        }
        if (!owns()) {
          throw new Error(supersededMessage);
        }
        const read = this.#connection.call<Read>({
          op: "step",
          statement: this.#live,
          count,
        });
        done = read.done;
        return rows.concat(read.rows);
      };
      const begin = (reset: string) => {
        if (stepped) {
          throw new Error(shiftedMessage(reset));
        }
        stepped = true;
      };
      const iterate = function* (): Generator<T, undefined> {
        stepped = true;
        for (
          let rows = pull(rowBatch);
          rows.length > 0;
          rows = pull(rowBatch)
        ) {
          yield* rows.map(compose);
        }
        return undefined;
      };
      return Object.defineProperties(iterate(), {
        lastInsertRowId: { value: ran.lastInsertRowId, enumerable: true },
        changes: { value: ran.changes, enumerable: true },
        getFirstSync: {
          value: (): T | null => {
            begin("resetSync");
            const [row] = pull(1);
            return row === undefined ? null : compose(row);
          },
          enumerable: true,
        },
        getAllSync: {
          value: (): T[] => {
            begin("resetSync");
            return pull(0).map(compose);
          },
          enumerable: true,
        },
        resetSync: {
          value: (): void => {
            if (!owns()) {
              throw new Error(supersededMessage);
            }
            this.#connection.call({ op: "reset", statement: this.#live });
            buffered = [];
            done = ran.columns.length === 0;
            stepped = false;
          },
          enumerable: true,
        },
      }) as unknown as SQLiteExecuteSyncResult<T>;
    }

    #asyncResult<T>(ran: Ran, raw: boolean): SQLiteExecuteAsyncResult<T> {
      const owns = this.#claim();
      const compose = composer<T>(ran.columns, raw);
      let buffered = ran.rows;
      let done = ran.done;
      let stepped = false;
      const pull = async (count: number): Promise<unknown[][]> => {
        const rows = buffered;
        buffered = [];
        if (done || (count !== 0 && rows.length > 0)) {
          return rows;
        }
        if (!owns()) {
          throw new Error(supersededMessage);
        }
        const read = await this.#connection.perform((send) =>
          send<Read>({ op: "step", statement: this.#live, count }),
        );
        done = read.done;
        return rows.concat(read.rows);
      };
      const begin = (reset: string) => {
        if (stepped) {
          throw new Error(shiftedMessage(reset));
        }
        stepped = true;
      };
      const iterate = async function* (): AsyncGenerator<T, undefined> {
        stepped = true;
        for (
          let rows = await pull(rowBatch);
          rows.length > 0;
          rows = await pull(rowBatch)
        ) {
          yield* rows.map(compose);
        }
        return undefined;
      };
      return Object.defineProperties(iterate(), {
        lastInsertRowId: { value: ran.lastInsertRowId, enumerable: true },
        changes: { value: ran.changes, enumerable: true },
        getFirstAsync: {
          value: async (): Promise<T | null> => {
            begin("resetAsync");
            const [row] = await pull(1);
            return row === undefined ? null : compose(row);
          },
          enumerable: true,
        },
        getAllAsync: {
          value: async (): Promise<T[]> => {
            begin("resetAsync");
            return (await pull(0)).map(compose);
          },
          enumerable: true,
        },
        resetAsync: {
          value: async (): Promise<void> => {
            if (!owns()) {
              throw new Error(supersededMessage);
            }
            await this.#connection.perform((send) =>
              send({ op: "reset", statement: this.#live }),
            );
            buffered = [];
            done = ran.columns.length === 0;
            stepped = false;
          },
          enumerable: true,
        },
      }) as unknown as SQLiteExecuteAsyncResult<T>;
    }

    executeSync<T>(...params: unknown[]): SQLiteExecuteSyncResult<T> {
      return this.#syncResult<T>(this.#runSync(params), false);
    }

    executeForRawResultSync<T>(
      ...params: unknown[]
    ): SQLiteExecuteSyncResult<T> {
      return this.#syncResult<T>(this.#runSync(params), true);
    }

    async executeAsync<T>(
      ...params: unknown[]
    ): Promise<SQLiteExecuteAsyncResult<T>> {
      return this.#asyncResult<T>(await this.#runAsync(params), false);
    }

    async executeForRawResultAsync<T>(
      ...params: unknown[]
    ): Promise<SQLiteExecuteAsyncResult<T>> {
      return this.#asyncResult<T>(await this.#runAsync(params), true);
    }

    getColumnNamesSync(): string[] {
      return [...this.#columns];
    }

    async getColumnNamesAsync(): Promise<string[]> {
      return this.getColumnNamesSync();
    }

    finalizeSync(): void {
      if (this.#id !== null) {
        const statement = this.#id;
        this.#id = null;
        this.#connection.call({ op: "finalize", statement });
      }
    }

    async finalizeAsync(): Promise<void> {
      if (this.#id !== null) {
        const statement = this.#id;
        this.#id = null;
        await this.#connection.callAsync({ op: "finalize", statement });
      }
    }
  }

  class SQLiteDatabase {
    readonly databasePath: string;
    readonly options: SQLiteOpenOptions;
    #connection: Connection;

    constructor(options: SQLiteOpenOptions, opened: Opened) {
      this.databasePath = opened.path;
      this.options = options;
      this.#connection = new Connection(opened.handle);
      owners.set(this, this.#connection);
    }

    get databaseName(): string {
      return nameOf(this.databasePath);
    }

    isInTransactionSync(): boolean {
      return !this.#connection.call<boolean>({ op: "autocommit" });
    }

    async isInTransactionAsync(): Promise<boolean> {
      return !(await this.#connection.callAsync<boolean>({ op: "autocommit" }));
    }

    interruptSync(): void {
      bridge.request({ op: "interrupt", handle: this.#connection.open });
    }

    closeSync(): void {
      this.#connection.close();
    }

    closeAsync(): Promise<void> {
      return this.#connection.closeAsync();
    }

    execSync(source: string): void {
      this.#connection.call({ op: "exec", sql: String(source) });
    }

    async execAsync(source: string): Promise<void> {
      await this.#connection.callAsync({ op: "exec", sql: String(source) });
    }

    prepareSync(source: string): SQLiteStatement {
      return new SQLiteStatement(
        this.#connection,
        String(source),
        this.#connection.call<Prepared>({ op: "prepare", sql: String(source) }),
      );
    }

    async prepareAsync(source: string): Promise<SQLiteStatement> {
      return new SQLiteStatement(
        this.#connection,
        String(source),
        await this.#connection.callAsync<Prepared>({
          op: "prepare",
          sql: String(source),
        }),
      );
    }

    readonly sql = <T = unknown>(
      strings: TemplateStringsArray,
      ...values: unknown[]
    ): SQLiteTaggedQuery<T> => new SQLiteTaggedQuery<T>(this, strings, values);

    runSync(source: string, ...params: unknown[]): SQLiteRunResult {
      const { lastInsertRowId, changes } = querySync(
        this.#connection,
        source,
        params,
        1,
      );
      return { lastInsertRowId, changes };
    }

    async runAsync(
      source: string,
      ...params: unknown[]
    ): Promise<SQLiteRunResult> {
      const { lastInsertRowId, changes } = await queryAsync(
        this.#connection,
        source,
        params,
        1,
      );
      return { lastInsertRowId, changes };
    }

    getFirstSync<T>(source: string, ...params: unknown[]): T | null {
      return firstOf<T>(querySync(this.#connection, source, params, 1), false);
    }

    async getFirstAsync<T>(
      source: string,
      ...params: unknown[]
    ): Promise<T | null> {
      return firstOf<T>(
        await queryAsync(this.#connection, source, params, 1),
        false,
      );
    }

    getAllSync<T>(source: string, ...params: unknown[]): T[] {
      return allOf<T>(querySync(this.#connection, source, params, 0), false);
    }

    async getAllAsync<T>(source: string, ...params: unknown[]): Promise<T[]> {
      return allOf<T>(
        await queryAsync(this.#connection, source, params, 0),
        false,
      );
    }

    *getEachSync<T>(source: string, ...params: unknown[]): IterableIterator<T> {
      const statement = this.prepareSync(source);
      try {
        yield* statement.executeSync<T>(...params);
      } finally {
        statement.finalizeSync();
      }
    }

    async *getEachAsync<T>(
      source: string,
      ...params: unknown[]
    ): AsyncIterableIterator<T> {
      const statement = await this.prepareAsync(source);
      try {
        yield* await statement.executeAsync<T>(...params);
      } finally {
        await statement.finalizeAsync();
      }
    }

    withTransactionSync(task: () => void): void {
      this.execSync("BEGIN");
      try {
        task();
        this.execSync("COMMIT");
      } catch (error) {
        try {
          this.execSync("ROLLBACK");
        } catch {}
        throw error;
      }
    }

    async withTransactionAsync(task: () => Promise<void>): Promise<void> {
      await this.execAsync("BEGIN");
      try {
        await task();
        await this.execAsync("COMMIT");
      } catch (error) {
        await this.execAsync("ROLLBACK").catch(ignore);
        throw error;
      }
    }

    async withExclusiveTransactionAsync(
      task: (transaction: SQLiteDatabase) => Promise<void>,
    ): Promise<void> {
      const options = { ...this.options, useNewConnection: true };
      const transaction = new Transaction(
        options,
        await this.#connection.callAsync<Opened>({ op: "connect" }),
      );
      try {
        await transaction.execAsync("BEGIN");
        await task(transaction);
        await transaction.execAsync("COMMIT");
      } catch (error) {
        await transaction.execAsync("ROLLBACK").catch(ignore);
        throw error;
      } finally {
        await transaction.closeAsync();
      }
    }

    serializeSync(databaseName = "main"): Uint8Array {
      return decodeBase64(
        this.#connection.call<{ data: string }>({
          op: "serialize",
          schema: String(databaseName),
        }).data,
      ).slice();
    }

    async serializeAsync(databaseName = "main"): Promise<Uint8Array> {
      return decodeBase64(
        (
          await this.#connection.callAsync<{ data: string }>({
            op: "serialize",
            schema: String(databaseName),
          })
        ).data,
      ).slice();
    }

    loadExtensionSync(): never {
      throw new Error("SQLite extensions are not supported by Swipt");
    }

    loadExtensionAsync(): Promise<never> {
      return Promise.reject(
        new Error("SQLite extensions are not supported by Swipt"),
      );
    }

    createSessionSync(): never {
      throw new Error("SQLite sessions are not supported by Swipt");
    }

    createSessionAsync(): Promise<never> {
      return Promise.reject(
        new Error("SQLite sessions are not supported by Swipt"),
      );
    }

    syncLibSQL(): Promise<never> {
      return Promise.reject(new Error("libSQL is not supported by Swipt"));
    }
  }

  class Transaction extends SQLiteDatabase {}

  class SQLiteTaggedQuery<T = unknown> implements PromiseLike<
    T[] | SQLiteRunResult
  > {
    #connection: Connection;
    #database: SQLiteDatabase;
    #source: string;
    #params: unknown[];

    constructor(
      database: SQLiteDatabase,
      strings: readonly string[],
      values: unknown[],
    ) {
      this.#connection = connectionOf(database, "the database");
      this.#database = database;
      this.#source = Array.from(strings).join("?");
      this.#params = [...values];
    }

    then<Fulfilled = T[] | SQLiteRunResult, Rejected = never>(
      onfulfilled?:
        | ((value: T[] | SQLiteRunResult) => Fulfilled | PromiseLike<Fulfilled>)
        | null,
      onrejected?:
        ((reason: unknown) => Rejected | PromiseLike<Rejected>) | null,
    ): Promise<Fulfilled | Rejected> {
      return queryAsync(this.#connection, this.#source, [this.#params], 0)
        .then(taggedResultOf<T>)
        .then(onfulfilled, onrejected);
    }

    async values(): Promise<unknown[][]> {
      return allOf<unknown[]>(
        await queryAsync(this.#connection, this.#source, [this.#params], 0),
        true,
      );
    }

    async first(): Promise<T | null> {
      return firstOf<T>(
        await queryAsync(this.#connection, this.#source, [this.#params], 1),
        false,
      );
    }

    each(): AsyncIterableIterator<T> {
      return this.#database.getEachAsync<T>(this.#source, this.#params);
    }

    allSync(): T[] | SQLiteRunResult {
      return taggedResultOf<T>(
        querySync(this.#connection, this.#source, [this.#params], 0),
      );
    }

    valuesSync(): unknown[][] {
      return allOf<unknown[]>(
        querySync(this.#connection, this.#source, [this.#params], 0),
        true,
      );
    }

    firstSync(): T | null {
      return firstOf<T>(
        querySync(this.#connection, this.#source, [this.#params], 1),
        false,
      );
    }

    eachSync(): IterableIterator<T> {
      return this.#database.getEachSync<T>(this.#source, this.#params);
    }
  }

  class SQLiteSession {
    constructor() {
      throw new Error("SQLite sessions are not supported by Swipt");
    }
  }

  const checkName = (name: unknown): string => {
    if (typeof name !== "string" || name === "") {
      throw new TypeError("the database name must be a non-empty string");
    }
    return name;
  };

  const checkDirectory = (directory: string | undefined) => {
    if (directory !== undefined && directory !== bridge.defaultDirectory) {
      throw new Error(
        "Swipt keeps the databases of a swiplet in its data directory; pass a relative name instead of a directory",
      );
    }
  };

  const openCall = (
    name: string,
    options: SQLiteOpenOptions,
    directory: string | undefined,
  ) => {
    checkDirectory(directory);
    return {
      op: "open",
      path: checkName(name),
      changes: options.enableChangeListener === true,
    };
  };

  const openDatabaseSync = (
    name: string,
    options: SQLiteOpenOptions = {},
    directory?: string,
  ): SQLiteDatabase =>
    new SQLiteDatabase(
      options,
      bridge.request(openCall(name, options, directory)) as Opened,
    );

  const openDatabaseAsync = async (
    name: string,
    options: SQLiteOpenOptions = {},
    directory?: string,
  ): Promise<SQLiteDatabase> =>
    new SQLiteDatabase(
      options,
      (await bridge.requestAsync(openCall(name, options, directory))) as Opened,
    );

  const deserializeCall = (data: unknown, options: SQLiteOpenOptions) => ({
    op: "deserialize",
    data: encodeBase64(toBytes(data, "the serialized database")),
    changes: options.enableChangeListener === true,
  });

  const deserializeDatabaseSync = (
    data: Uint8Array,
    options: SQLiteOpenOptions = {},
  ): SQLiteDatabase =>
    new SQLiteDatabase(
      options,
      bridge.request(deserializeCall(data, options)) as Opened,
    );

  const deserializeDatabaseAsync = async (
    data: Uint8Array,
    options: SQLiteOpenOptions = {},
  ): Promise<SQLiteDatabase> =>
    new SQLiteDatabase(
      options,
      (await bridge.requestAsync(deserializeCall(data, options))) as Opened,
    );

  const deleteDatabaseSync = (name: string, directory?: string): void => {
    checkDirectory(directory);
    bridge.request({ op: "delete", path: checkName(name) });
  };

  const deleteDatabaseAsync = async (
    name: string,
    directory?: string,
  ): Promise<void> => {
    checkDirectory(directory);
    await bridge.requestAsync({ op: "delete", path: checkName(name) });
  };

  const importDatabaseFromAssetAsync = async (
    name: string,
    source: SQLiteProviderAssetSource,
    directory?: string,
  ): Promise<void> => {
    checkDirectory(directory);
    const asset: unknown = source?.assetId;
    const location =
      typeof asset === "string"
        ? asset
        : typeof asset === "object" &&
            asset !== null &&
            typeof (asset as { uri?: unknown }).uri === "string"
          ? (asset as { uri: string }).uri
          : undefined;
    if (location === undefined) {
      throw new TypeError(
        "assetId must be what requiring the database file returns, a file of assets, or a path in the assets directory",
      );
    }
    await bridge.requestAsync({
      op: "import",
      path: checkName(name),
      asset: location,
      overwrite: source.forceOverwrite === true,
    });
  };

  const backupCall = (options: SQLiteBackupOptions) => ({
    dest: connectionOf(options.destDatabase, "destDatabase"),
    source: connectionOf(options.sourceDatabase, "sourceDatabase"),
    call: {
      op: "backup",
      schema: String(options.destDatabaseName ?? "main"),
      sourceSchema: String(options.sourceDatabaseName ?? "main"),
    },
  });

  const backupDatabaseSync = (options: SQLiteBackupOptions): void => {
    const { dest, source, call } = backupCall(options);
    dest.call({ ...call, source: source.open });
  };

  const backupDatabaseAsync = async (
    options: SQLiteBackupOptions,
  ): Promise<void> => {
    const { dest, source, call } = backupCall(options);
    await schedule([...new Set([dest, source])], () =>
      bridge.requestAsync({ ...call, handle: dest.open, source: source.open }),
    );
  };

  const listeners = new Set<(event: DatabaseChangeEvent) => void>();
  let stop: (() => void) | undefined;

  const addDatabaseChangeListener = (
    listener: (event: DatabaseChangeEvent) => void,
  ) => {
    if (typeof listener !== "function") {
      throw new TypeError("the listener is not a function");
    }
    listeners.add(listener);
    if (!closed) {
      stop ??= bridge.subscribe((event) => {
        for (const current of [...listeners]) {
          try {
            current(event);
          } catch (error) {
            console.error("a database change listener threw", error);
          }
        }
      });
    }
    let removed = false;
    return {
      remove() {
        if (removed) {
          return;
        }
        removed = true;
        listeners.delete(listener);
        if (listeners.size === 0 && stop !== undefined) {
          const current = stop;
          stop = undefined;
          current();
        }
      },
    };
  };

  const storageIndex = (index: unknown): number | null => {
    const value = Math.floor(Number(index));
    if (!Number.isSafeInteger(value)) {
      return 0;
    }
    return value < 0 ? null : value;
  };

  const checkStorageInput = (...input: unknown[]) => {
    const [key, value] = input;
    if (typeof key !== "string") {
      throw new TypeError(`a storage key must be a string, not ${typeof key}`);
    }
    if (
      input.length > 1 &&
      typeof value !== "string" &&
      typeof value !== "function"
    ) {
      throw new TypeError(
        `a storage value must be a string, not ${typeof value}`,
      );
    }
  };

  const mergeDeep = (target: unknown, source: unknown): unknown => {
    if (
      typeof target !== "object" ||
      target === null ||
      typeof source !== "object" ||
      source === null
    ) {
      return typeof target !== "object" || target === null ? source : target;
    }
    const output: Record<string, unknown> = { ...target };
    for (const [key, value] of Object.entries(source)) {
      if (Array.isArray(value)) {
        output[key] = ((output[key] as unknown[] | undefined) ?? []).concat(
          value,
        );
      } else if (typeof value === "object") {
        output[key] = mergeDeep(
          (target as Record<string, unknown>)[key],
          value,
        );
      } else {
        output[key] = value;
      }
    }
    return output;
  };

  const storageSchema =
    "CREATE TABLE IF NOT EXISTS storage (key TEXT PRIMARY KEY NOT NULL, value TEXT)";
  const storageGet = "SELECT value FROM storage WHERE key = ?";
  const storageSet =
    "INSERT INTO storage (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value";
  const storageRemove = "DELETE FROM storage WHERE key = ?";

  class SQLiteStorage {
    #name: string;
    #database: SQLiteDatabase | null = null;
    #opening: Promise<SQLiteDatabase> | null = null;

    constructor(databaseName: string) {
      this.#name = checkName(databaseName);
    }

    #databaseSync(): SQLiteDatabase {
      if (this.#database === null) {
        const database = openDatabaseSync(this.#name);
        try {
          database.withTransactionSync(() => {
            const version =
              database.getFirstSync<{ user_version: number }>(
                "PRAGMA user_version",
              )?.user_version ?? 0;
            database.execSync(storageSchema);
            if (version < 1) {
              database.execSync("PRAGMA user_version = 1");
            }
          });
        } catch (error) {
          database.closeSync();
          throw error;
        }
        this.#database = database;
      }
      return this.#database;
    }

    #databaseAsync(): Promise<SQLiteDatabase> {
      if (this.#database !== null) {
        return Promise.resolve(this.#database);
      }
      this.#opening ??= (async () => {
        try {
          const database = await openDatabaseAsync(this.#name);
          try {
            await database.withTransactionAsync(async () => {
              const version =
                (
                  await database.getFirstAsync<{ user_version: number }>(
                    "PRAGMA user_version",
                  )
                )?.user_version ?? 0;
              await database.execAsync(storageSchema);
              if (version < 1) {
                await database.execAsync("PRAGMA user_version = 1");
              }
            });
          } catch (error) {
            await database.closeAsync();
            throw error;
          }
          this.#database = database;
          return database;
        } finally {
          this.#opening = null;
        }
      })();
      return this.#opening;
    }

    async getItemAsync(key: string): Promise<string | null> {
      checkStorageInput(key);
      return (
        (
          await (
            await this.#databaseAsync()
          ).getFirstAsync<{ value: string }>(storageGet, key)
        )?.value ?? null
      );
    }

    async setItemAsync(
      key: string,
      value: string | SQLiteStorageSetItemUpdateFunction,
    ): Promise<void> {
      checkStorageInput(key, value);
      const database = await this.#databaseAsync();
      if (typeof value !== "function") {
        await database.runAsync(storageSet, key, value);
        return;
      }
      await database.withExclusiveTransactionAsync(async (transaction) => {
        const next = value(
          (await transaction.getFirstAsync<{ value: string }>(storageGet, key))
            ?.value ?? null,
        );
        checkStorageInput(key, next);
        await transaction.runAsync(storageSet, key, next);
      });
    }

    async removeItemAsync(key: string): Promise<boolean> {
      checkStorageInput(key);
      return (
        (await (await this.#databaseAsync()).runAsync(storageRemove, key))
          .changes > 0
      );
    }

    async getAllKeysAsync(): Promise<string[]> {
      return (
        await (
          await this.#databaseAsync()
        ).getAllAsync<{ key: string }>("SELECT key FROM storage")
      ).map(({ key }) => key);
    }

    async clearAsync(): Promise<boolean> {
      return (
        (await (await this.#databaseAsync()).runAsync("DELETE FROM storage"))
          .changes > 0
      );
    }

    async getLengthAsync(): Promise<number> {
      return (
        (
          await (
            await this.#databaseAsync()
          ).getFirstAsync<{ count: number }>(
            "SELECT count(*) AS count FROM storage",
          )
        )?.count ?? 0
      );
    }

    async getKeyByIndexAsync(index: number): Promise<string | null> {
      const offset = storageIndex(index);
      if (offset === null) {
        return null;
      }
      return (
        (
          await (
            await this.#databaseAsync()
          ).getFirstAsync<{ key: string }>(
            "SELECT key FROM storage LIMIT 1 OFFSET ?",
            offset,
          )
        )?.key ?? null
      );
    }

    async closeAsync(): Promise<void> {
      await this.#opening?.catch(ignore);
      const database = this.#database;
      this.#database = null;
      await database?.closeAsync();
    }

    getItemSync(key: string): string | null {
      checkStorageInput(key);
      return (
        this.#databaseSync().getFirstSync<{ value: string }>(storageGet, key)
          ?.value ?? null
      );
    }

    setItemSync(
      key: string,
      value: string | SQLiteStorageSetItemUpdateFunction,
    ): void {
      checkStorageInput(key, value);
      const database = this.#databaseSync();
      if (typeof value !== "function") {
        database.runSync(storageSet, key, value);
        return;
      }
      database.withTransactionSync(() => {
        const next = value(
          database.getFirstSync<{ value: string }>(storageGet, key)?.value ??
            null,
        );
        checkStorageInput(key, next);
        database.runSync(storageSet, key, next);
      });
    }

    removeItemSync(key: string): boolean {
      checkStorageInput(key);
      return this.#databaseSync().runSync(storageRemove, key).changes > 0;
    }

    getAllKeysSync(): string[] {
      return this.#databaseSync()
        .getAllSync<{ key: string }>("SELECT key FROM storage")
        .map(({ key }) => key);
    }

    clearSync(): boolean {
      return this.#databaseSync().runSync("DELETE FROM storage").changes > 0;
    }

    getLengthSync(): number {
      return (
        this.#databaseSync().getFirstSync<{ count: number }>(
          "SELECT count(*) AS count FROM storage",
        )?.count ?? 0
      );
    }

    getKeyByIndexSync(index: number): string | null {
      const offset = storageIndex(index);
      if (offset === null) {
        return null;
      }
      return (
        this.#databaseSync().getFirstSync<{ key: string }>(
          "SELECT key FROM storage LIMIT 1 OFFSET ?",
          offset,
        )?.key ?? null
      );
    }

    closeSync(): void {
      const database = this.#database;
      this.#database = null;
      database?.closeSync();
    }

    getItem(key: string): Promise<string | null> {
      return this.getItemAsync(key);
    }

    setItem(
      key: string,
      value: string | SQLiteStorageSetItemUpdateFunction,
    ): Promise<void> {
      return this.setItemAsync(key, value);
    }

    async removeItem(key: string): Promise<void> {
      await this.removeItemAsync(key);
    }

    getAllKeys(): Promise<string[]> {
      return this.getAllKeysAsync();
    }

    async clear(): Promise<void> {
      await this.clearAsync();
    }

    async mergeItem(key: string, value: string): Promise<void> {
      checkStorageInput(key, value);
      await this.setItemAsync(key, (previous) =>
        previous === null
          ? value
          : JSON.stringify(mergeDeep(JSON.parse(previous), JSON.parse(value))),
      );
    }

    multiGet(keys: string[]): Promise<[string, string | null][]> {
      return Promise.all(
        keys.map(async (key): Promise<[string, string | null]> => [
          key,
          await this.getItemAsync(key),
        ]),
      );
    }

    async multiSet(pairs: [string, string][]): Promise<void> {
      await (
        await this.#databaseAsync()
      ).withExclusiveTransactionAsync(async (transaction) => {
        for (const [key, value] of pairs) {
          checkStorageInput(key, value);
          await transaction.runAsync(storageSet, key, value);
        }
      });
    }

    async multiRemove(keys: string[]): Promise<void> {
      await (
        await this.#databaseAsync()
      ).withExclusiveTransactionAsync(async (transaction) => {
        for (const key of keys) {
          checkStorageInput(key);
          await transaction.runAsync(storageRemove, key);
        }
      });
    }

    async multiMerge(pairs: [string, string][]): Promise<void> {
      await (
        await this.#databaseAsync()
      ).withExclusiveTransactionAsync(async (transaction) => {
        for (const [key, value] of pairs) {
          checkStorageInput(key, value);
          const previous = (
            await transaction.getFirstAsync<{ value: string }>(storageGet, key)
          )?.value;
          await transaction.runAsync(
            storageSet,
            key,
            previous === undefined
              ? value
              : JSON.stringify(
                  mergeDeep(JSON.parse(previous), JSON.parse(value)),
                ),
          );
        }
      });
    }

    close(): Promise<void> {
      return this.closeAsync();
    }
  }

  let asyncStorage: SQLiteStorage | undefined;
  const defaultStorage = () =>
    (asyncStorage ??= new SQLiteStorage("ExpoSQLiteStorage"));

  const kvStore = Object.freeze({
    __esModule: true,
    SQLiteStorage,
    get AsyncStorage() {
      return defaultStorage();
    },
    get Storage() {
      return defaultStorage();
    },
    get default() {
      return defaultStorage();
    },
  });

  class Storage {
    clear(): void {
      defaultStorage().clearSync();
    }

    getItem(key: string): string | null {
      return defaultStorage().getItemSync(key);
    }

    key(index: number): string | null {
      return defaultStorage().getKeyByIndexSync(index);
    }

    removeItem(key: string): void {
      defaultStorage().removeItemSync(key);
    }

    setItem(key: string, value: string): void {
      defaultStorage().setItemSync(key, String(value));
    }

    get length(): number {
      return defaultStorage().getLengthSync();
    }

    toString(): string {
      return "[object Storage]";
    }
  }

  const builtin = new Set<PropertyKey>([
    ...Object.getOwnPropertyNames(Storage.prototype),
    ...Object.getOwnPropertyNames(Object.prototype),
  ]);
  const localStorage = new Proxy(new Storage(), {
    get: (target, key, receiver) =>
      typeof key !== "string" || builtin.has(key)
        ? Reflect.get(target, key, receiver)
        : (target.getItem(key) ?? undefined),
    set: (target, key, value, receiver) => {
      if (typeof key !== "string" || builtin.has(key)) {
        return Reflect.set(target, key, value, receiver);
      }
      target.setItem(key, String(value));
      return true;
    },
    deleteProperty: (target, key) => {
      if (typeof key !== "string" || Reflect.ownKeys(target).includes(key)) {
        return Reflect.deleteProperty(target, key);
      }
      target.removeItem(key);
      return true;
    },
    has: (target, key) =>
      typeof key !== "string" || builtin.has(key)
        ? Reflect.has(target, key)
        : target.getItem(key) !== null,
  });

  const exports = Object.freeze({
    __esModule: true,
    SQLiteDatabase,
    SQLiteStatement,
    SQLiteTaggedQuery,
    SQLiteSession,
    bundledExtensions: Object.freeze({}),
    configureWeb: (_options?: unknown): void => {},
    deepEqual,
    importDatabaseFromAssetAsync,
    openDatabaseSync,
    openDatabaseAsync,
    deleteDatabaseSync,
    deleteDatabaseAsync,
    deserializeDatabaseSync,
    deserializeDatabaseAsync,
    backupDatabaseSync,
    backupDatabaseAsync,
    addDatabaseChangeListener,
    defaultDatabaseDirectory: bridge.defaultDirectory,
  });

  const close = () => {
    closed = true;
    for (const connection of [...connections]) {
      connection.discard();
    }
    listeners.clear();
    stop?.();
    stop = undefined;
  };

  return { exports, kvStore, localStorage, close };
}
