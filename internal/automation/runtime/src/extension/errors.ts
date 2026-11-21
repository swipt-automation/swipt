import { inspect } from "../shared/inspect";
import {
  brand,
  defineInterface,
  dictionary,
  domString,
  double,
  optional,
} from "../shared/webidl";
import { framesOf, type Frame } from "./format";

const codes: Record<string, number> = {
  IndexSizeError: 1,
  HierarchyRequestError: 3,
  WrongDocumentError: 4,
  InvalidCharacterError: 5,
  NoModificationAllowedError: 7,
  NotFoundError: 8,
  NotSupportedError: 9,
  InUseAttributeError: 10,
  InvalidStateError: 11,
  SyntaxError: 12,
  InvalidModificationError: 13,
  NamespaceError: 14,
  InvalidAccessError: 15,
  TypeMismatchError: 17,
  SecurityError: 18,
  NetworkError: 19,
  AbortError: 20,
  URLMismatchError: 21,
  QuotaExceededError: 22,
  TimeoutError: 23,
  InvalidNodeTypeError: 24,
  DataCloneError: 25,
};

const legacyCodes = {
  INDEX_SIZE_ERR: 1,
  DOMSTRING_SIZE_ERR: 2,
  HIERARCHY_REQUEST_ERR: 3,
  WRONG_DOCUMENT_ERR: 4,
  INVALID_CHARACTER_ERR: 5,
  NO_DATA_ALLOWED_ERR: 6,
  NO_MODIFICATION_ALLOWED_ERR: 7,
  NOT_FOUND_ERR: 8,
  NOT_SUPPORTED_ERR: 9,
  INUSE_ATTRIBUTE_ERR: 10,
  INVALID_STATE_ERR: 11,
  SYNTAX_ERR: 12,
  INVALID_MODIFICATION_ERR: 13,
  NAMESPACE_ERR: 14,
  INVALID_ACCESS_ERR: 15,
  VALIDATION_ERR: 16,
  TYPE_MISMATCH_ERR: 17,
  SECURITY_ERR: 18,
  NETWORK_ERR: 19,
  ABORT_ERR: 20,
  URL_MISMATCH_ERR: 21,
  QUOTA_EXCEEDED_ERR: 22,
  TIMEOUT_ERR: 23,
  INVALID_NODE_TYPE_ERR: 24,
  DATA_CLONE_ERR: 25,
} as const;

type LegacyCodes = typeof legacyCodes;

export const DOMException = defineInterface(
  class DOMException extends (Error as unknown as (new () => Error &
    LegacyCodes) &
    LegacyCodes) {
    readonly #name: string;
    readonly #message: string;

    static [brand](value: object) {
      return #name in value;
    }

    constructor(message?: string, name?: string) {
      super();
      this.#message = message!;
      this.#name = name!;
    }

    get name(): string {
      return this.#name;
    }

    get message(): string {
      return this.#message;
    }

    get code(): number {
      return Object.hasOwn(codes, this.#name) ? codes[this.#name] : 0;
    }
  },
  {
    construct: [optional(domString, ""), optional(domString, "Error")],
    constants: legacyCodes,
  },
);
export type DOMException = InstanceType<typeof DOMException>;

const quotaOptions = dictionary<{ quota?: number; requested?: number }>({
  quota: double,
  requested: double,
});

export const QuotaExceededError = defineInterface(
  class QuotaExceededError extends DOMException {
    readonly #quota: number | null;
    readonly #requested: number | null;

    static [brand](value: object) {
      return #quota in value;
    }

    constructor(
      message?: string,
      options?: { quota?: number; requested?: number },
    ) {
      const { quota, requested } = options!;
      if (quota !== undefined && quota < 0) {
        throw new RangeError("the quota is negative");
      }
      if (requested !== undefined && requested < 0) {
        throw new RangeError("the requested amount is negative");
      }
      if (quota !== undefined && requested !== undefined && requested < quota) {
        throw new RangeError("the requested amount is below the quota");
      }
      super(message, "QuotaExceededError");
      this.#quota = quota ?? null;
      this.#requested = requested ?? null;
    }

    get quota(): number | null {
      return this.#quota;
    }

    get requested(): number | null {
      return this.#requested;
    }
  },
  {
    construct: [optional(domString, ""), optional(quotaOptions)],
  },
);
export type QuotaExceededError = InstanceType<typeof QuotaExceededError>;

export interface Failure {
  name?: string;
  message?: string;
  stack?: Frame[];
}

export function errorFrom({ name = "Error", message = "" }: Failure): Error {
  switch (name) {
    case "AbortError":
    case "TimeoutError":
    case "UnavailableError":
    case "NotSupportedError":
    case "NotAllowedError":
    case "NotFoundError":
    case "InvalidStateError":
    case "DataError":
      return new DOMException(message, name);
    case "TypeError":
      return new TypeError(message);
    case "RangeError":
      return new RangeError(message);
    case "SyntaxError":
      return new SyntaxError(message);
  }
  const error = new Error(message);
  if (name !== "Error") {
    Object.defineProperty(error, "name", {
      value: name,
      configurable: true,
      writable: true,
    });
  }
  return error;
}

export function failureOf(error: unknown): string {
  try {
    if (error instanceof Error) {
      return JSON.stringify({
        name: String(error.name),
        message: String(error.message),
        stack: framesOf(error),
      });
    }
    return JSON.stringify({
      name: "Error",
      message: typeof error === "string" ? error : inspect(error),
    });
  } catch {
    return JSON.stringify({
      name: "Error",
      message: "the thrown value could not be described",
    });
  }
}

export const isAbort = (error: unknown): boolean =>
  error instanceof Error && error.name === "AbortError";
