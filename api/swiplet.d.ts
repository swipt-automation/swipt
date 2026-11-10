import type {
  StandardJSONSchemaV1,
  StandardSchemaV1,
} from "@standard-schema/spec";

import type { Jsonify } from "./index";

export type ActionCaller = "app" | "shortcuts" | "api";

export type Schema<Input = unknown, Output = Input> = StandardSchemaV1<
  Input,
  Output
> &
  StandardJSONSchemaV1<Input, Output>;

declare const types: unique symbol;

export interface Action<Input = void, Output = void> {
  readonly [types]: { input: Input; output: Output };
  readonly title: string;
  readonly description?: string;
  readonly expose: readonly "shortcuts"[];
}

export interface Extension<
  Actions extends Record<string, Action<any, any>> = {},
  HasRun extends boolean = boolean,
> {
  readonly [types]: { hasRun: HasRun };
  readonly actions: Actions;
}

export interface RunContext {
  readonly signal: AbortSignal;
}

export interface ActionContext extends RunContext {
  readonly caller: ActionCaller;
}

type Received<S> = S extends Schema ? StandardSchemaV1.InferOutput<S> : void;
type Sent<S> = S extends Schema ? StandardSchemaV1.InferInput<S> : void;
type Returned<S, R> = S extends Schema ? StandardSchemaV1.InferInput<S> : R;
type Delivered<S, R> = S extends Schema ? StandardSchemaV1.InferOutput<S> : R;

export declare function defineAction<
  I extends Schema | undefined = undefined,
  O extends Schema | undefined = undefined,
  R = void,
>(definition: {
  readonly title: string;
  readonly description?: string;
  readonly input?: I;
  readonly output?: O;
  readonly expose?: readonly "shortcuts"[];
  run(
    input: Received<I>,
    context: ActionContext,
  ): Returned<O, R> | Promise<Returned<O, R>>;
}): Action<Sent<I>, Jsonify<Awaited<Delivered<O, R>>>>;
export declare function defineAction<
  I extends Schema | undefined = undefined,
  O extends Schema | undefined = undefined,
  T = void,
>(definition: {
  readonly title: string;
  readonly description?: string;
  readonly input?: I;
  readonly output?: O;
  /** @available apiLevel 3 */
  stream(
    input: Received<I>,
    context: ActionContext,
  ): AsyncIterable<Returned<O, T>>;
}): Action<Sent<I>, AsyncIterable<Jsonify<Delivered<O, T>>>>;

export declare function defineExtension<
  Actions extends Record<string, Action<any, any>> = {},
>(definition: {
  run(context: RunContext): Promise<void>;
  readonly actions?: Actions;
}): Extension<Actions, true>;
export declare function defineExtension<
  Actions extends Record<string, Action<any, any>> = {},
>(definition: { readonly actions?: Actions }): Extension<Actions, false>;

export type ServiceStatus = "starting" | "running" | "stopped" | "failed";

type ActionReturn<Output> = [Output] extends [never]
  ? Promise<Output>
  : Output extends AsyncIterable<unknown>
    ? Output
    : Promise<Output>;

type ActionCall<A> =
  A extends Action<infer Input, infer Output>
    ? [Input] extends [void]
      ? (
          input?: undefined,
          options?: { signal?: AbortSignal },
        ) => ActionReturn<Output>
      : (
          input: Input,
          options?: { signal?: AbortSignal },
        ) => ActionReturn<Output>
    : never;

export type ActionCalls<E extends Extension<any, any>> = {
  readonly [K in keyof E["actions"]]: ActionCall<E["actions"][K]>;
};

export interface ServiceHandle {
  readonly enabled: boolean;
  readonly status: ServiceStatus;
  readonly error: string | null;
  readonly since: Date | null;
  setEnabled(enabled: boolean): Promise<void>;
}

export declare function actions<
  E extends Extension<any, any>,
>(): ActionCalls<E>;

export declare function useService<
  E extends Extension<any, true>,
>(): ServiceHandle;

export declare function exit(): void;

export {};
