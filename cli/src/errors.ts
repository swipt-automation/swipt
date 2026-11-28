export const Code = {
  Canceled: 1,
  Unknown: 2,
  DeadlineExceeded: 4,
  NotFound: 5,
  Unimplemented: 12,
  Internal: 13,
  Unavailable: 14,
  Unauthenticated: 16,
};

export class UsageError extends Error {}

export class RpcError extends Error {
  declare code: number;

  constructor(code: number, message?: string) {
    super(message);
    this.code = code;
  }
}

export class FalseResult extends Error {}

export const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : String(error);

export const withToken = (url: string, token = "TOKEN") =>
  url.replace("://", `://${encodeURIComponent(token)}@`);
