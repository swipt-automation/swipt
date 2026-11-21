const abortError = (signal: AbortSignal): unknown =>
  signal.reason ??
  Object.assign(new Error("the call was aborted"), { name: "AbortError" });

export const untilAborted = <T>(
  signal: AbortSignal | undefined,
  start: () => Promise<T>,
): Promise<T> => {
  if (signal?.aborted) {
    return Promise.reject(abortError(signal));
  }
  const reply = start();
  if (signal === undefined) {
    return reply;
  }
  return new Promise((resolve, reject) => {
    const abort = () => reject(abortError(signal));
    signal.addEventListener("abort", abort, { once: true });
    reply
      .then(resolve, reject)
      .finally(() => signal.removeEventListener("abort", abort));
  });
};
