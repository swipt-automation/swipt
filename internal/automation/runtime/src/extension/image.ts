import type { Point, Rect } from "@swipt/api";
import type {
  ConnectedComponent,
  DetectedBarcode,
  ImageDifference,
  ImageFormat,
  ImageStats,
  RasterImage as RasterImageInterface,
  RecognizedTextBox,
  RecognizedTextLine,
  TemplateMatch,
  TextRecognitionOptions,
} from "@swipt/api/image";

import { invoke } from "./async";
import { DOMException } from "./errors";
import { fileOf, locationOf } from "./files";
import { native, type GraphicsNative } from "./native";
import { check, optionsOf, signalOf, type Options } from "./options";

type Color = [red: number, green: number, blue: number];

export interface ImageHandle {
  readonly width: number;
  readonly height: number;
  readonly scale: number;
  readonly frame: number[];
  readonly time: number;
  readonly timestamp: number;
  readonly pixelWidth: number;
  readonly pixelHeight: number;
  crop(x: number, y: number, width: number, height: number): ImageHandle;
  resize(width: number, height: number): ImageHandle;
  rotate(degrees: number): ImageHandle;
  grayscale(): ImageHandle;
  threshold(level: number): ImageHandle;
  mask(
    lowRed: number,
    lowGreen: number,
    lowBlue: number,
    highRed: number,
    highGreen: number,
    highBlue: number,
  ): ImageHandle;
  invert(): ImageHandle;
  dilate(radius: number): ImageHandle;
  erode(radius: number): ImageHandle;
  showBytes(): Uint8Array;
}

const largestSide = 16384;
const defaultTolerance = 16;

export const graphicsNative = (): GraphicsNative => {
  const graphics = native.graphics;
  if (graphics === undefined) {
    throw new DOMException(
      "graphics are not available on this platform",
      "NotSupportedError",
    );
  }
  return graphics;
};

export const run = <T>(
  method: string,
  args: Record<string, unknown>,
  objects: object[],
  signal?: AbortSignal,
): Promise<T> => {
  if (signal?.aborted) {
    return Promise.reject(signal.reason);
  }
  const id = graphicsNative().prepare(method, JSON.stringify(args), objects);
  return invoke<T>("graphics", [id], { signal });
};

export const bytesOf = (id: number): Uint8Array => {
  const bytes = graphicsNative().bytes(id);
  if (bytes === null) {
    throw new DOMException("the result was released", "InvalidStateError");
  }
  return bytes;
};

export const finite = (value: unknown, name: string): number => {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new TypeError(`${name} must be a finite number`);
  }
  return value;
};

const positive = (value: unknown, name: string): number => {
  if (finite(value, name) <= 0) {
    throw new RangeError(`${name} must be positive`);
  }
  return value as number;
};

const integer = (
  value: unknown,
  name: string,
  low: number,
  high: number,
): number => {
  if (
    !Number.isInteger(value) ||
    (value as number) < low ||
    (value as number) > high
  ) {
    throw new RangeError(`${name} must be an integer from ${low} to ${high}`);
  }
  return value as number;
};

export const pointOf = (value: unknown, name: string): Point => {
  if (
    !Array.isArray(value) ||
    value.length !== 2 ||
    !Array.from(value).every(Number.isFinite)
  ) {
    throw new TypeError(`${name} must be [x, y] of finite numbers`);
  }
  return [value[0], value[1]];
};

export const rectOf = (value: unknown, name: string): Rect => {
  if (
    !Array.isArray(value) ||
    value.length !== 4 ||
    !Array.from(value).every(Number.isFinite)
  ) {
    throw new TypeError(
      `${name} must be [x, y, width, height] of finite numbers`,
    );
  }
  if (value[2] <= 0 || value[3] <= 0) {
    throw new RangeError(`${name} needs a positive width and height`);
  }
  return [value[0], value[1], value[2], value[3]];
};

export const timeoutOf = (value: unknown): number | null => {
  if (value === undefined) {
    return 10_000;
  }
  if (value === Infinity) {
    return null;
  }
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    throw new TypeError(
      "timeoutMs must be a non-negative number of milliseconds, or Infinity",
    );
  }
  return value;
};

const colorOf = (value: unknown, name: string): Color => {
  if (typeof value === "string") {
    const match = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(value);
    if (match) {
      const hex =
        match[1].length === 3
          ? [...match[1]].map((digit) => digit + digit).join("")
          : match[1];
      return [
        parseInt(hex.slice(0, 2), 16),
        parseInt(hex.slice(2, 4), 16),
        parseInt(hex.slice(4, 6), 16),
      ];
    }
  } else if (
    Array.isArray(value) &&
    value.length === 3 &&
    Array.from(value).every(
      (item) => Number.isInteger(item) && item >= 0 && item <= 255,
    )
  ) {
    return [value[0], value[1], value[2]];
  }
  throw new TypeError(
    `${name} must be "#rrggbb" or [red, green, blue] of integers from 0 to 255`,
  );
};

const toleranceOf = (value: unknown): number =>
  value === undefined ? defaultTolerance : integer(value, "tolerance", 0, 255);

const clampChannel = (value: number) => Math.min(255, Math.max(0, value));

const freeze = <T>(value: T): T => {
  if (value !== null && typeof value === "object") {
    for (const item of Object.values(value)) {
      freeze(item);
    }
    Object.freeze(value);
  }
  return value;
};

const handles = new WeakMap<RasterImage, ImageHandle>();

export const handleOf = (image: unknown, name = "the image"): ImageHandle => {
  const handle = image instanceof RasterImage ? handles.get(image) : undefined;
  if (handle === undefined) {
    throw new TypeError(`${name} must be a RasterImage`);
  }
  return handle;
};

export const adopt = (id: number): RasterImage => {
  const handle = graphicsNative().take(id);
  if (handle === null) {
    throw new DOMException("the image was released", "InvalidStateError");
  }
  return new RasterImage(handle as ImageHandle);
};

export const adoptOptional = (id: unknown): RasterImage | undefined => {
  const handle = typeof id === "number" ? graphicsNative().take(id) : null;
  return handle === null ? undefined : new RasterImage(handle as ImageHandle);
};

const strings = (value: unknown, name: string): string[] => {
  if (
    !Array.isArray(value) ||
    !Array.from(value).every((item) => typeof item === "string")
  ) {
    throw new TypeError(`${name} must be an array of strings`);
  }
  return value;
};

const booleanOf = (value: unknown, name: string): boolean | undefined => {
  if (value !== undefined && typeof value !== "boolean") {
    throw new TypeError(`${name} must be a boolean`);
  }
  return value as boolean | undefined;
};

const textOptionsOf = (options: unknown): Record<string, unknown> => {
  const {
    region,
    languages,
    detectLanguage,
    level,
    words,
    correction,
    minimumHeight,
  } = optionsOf(options, [
    "region",
    "languages",
    "detectLanguage",
    "level",
    "words",
    "correction",
    "minimumHeight",
  ]);
  if (level !== undefined && level !== "fast" && level !== "accurate") {
    throw new TypeError("level must be fast or accurate");
  }
  return {
    region: region === undefined ? undefined : rectOf(region, "region"),
    languages:
      languages === undefined ? undefined : strings(languages, "languages"),
    detectLanguage: booleanOf(detectLanguage, "detectLanguage"),
    level,
    words: words === undefined ? undefined : strings(words, "words"),
    correction: booleanOf(correction, "correction"),
    minimumHeight:
      minimumHeight === undefined
        ? undefined
        : positive(minimumHeight, "minimumHeight"),
  };
};

interface TextHandle {
  boundingBox(
    line: number,
    candidate: number,
    start: number,
    end: number,
  ): RecognizedTextBox | null;
}

interface RawLine extends RecognizedTextBox {
  text: string;
  confidence: number;
  candidates: { text: string; confidence: number }[];
}

const linesOf = (result: {
  lines: RawLine[];
  text: number;
}): RecognizedTextLine[] => {
  const handle = graphicsNative().take(result.text) as TextHandle | null;
  if (handle === null) {
    throw new DOMException("the text was released", "InvalidStateError");
  }
  const boxed = <T extends object>(
    value: T,
    text: string,
    line: number,
    candidate: number,
  ): T =>
    Object.defineProperty(value, "boundingBox", {
      value: (start: number, end: number): RecognizedTextBox | null => {
        const from = integer(start, "start", 0, text.length);
        return freeze(
          handle.boundingBox(
            line,
            candidate,
            from,
            integer(end, "end", from, text.length),
          ),
        );
      },
    });
  return result.lines.map(({ candidates, ...line }, index) =>
    freeze(
      boxed(
        {
          ...line,
          candidates: candidates.map((candidate, rank) =>
            boxed({ ...candidate }, candidate.text, index, rank),
          ),
        },
        line.text,
        index,
        0,
      ) as unknown as RecognizedTextLine,
    ),
  );
};

const formatOf = (value: unknown): ImageFormat => {
  if (value !== "png" && value !== "jpeg" && value !== "heic") {
    throw new TypeError("the format must be png, jpeg or heic");
  }
  return value;
};

const qualityOf = (value: unknown): number | undefined => {
  if (value === undefined) {
    return undefined;
  }
  if (typeof value !== "number" || !(value >= 0 && value <= 1)) {
    throw new RangeError("quality must be from 0 to 1");
  }
  return value;
};

const matchOptionsOf = (options: unknown, extra: string[]) => {
  const { threshold, region, scales, limit, overlap } = optionsOf(options, [
    "threshold",
    "region",
    "scales",
    ...extra,
  ]);
  const out: Record<string, unknown> = {};
  if (threshold !== undefined) {
    if (typeof threshold !== "number" || !(threshold >= -1 && threshold <= 1)) {
      throw new RangeError("threshold must be from -1 to 1");
    }
    out.threshold = threshold;
  }
  if (region !== undefined) {
    out.region = rectOf(region, "region");
  }
  if (scales !== undefined) {
    if (!Array.isArray(scales) || scales.length === 0) {
      throw new TypeError(
        "scales must be a non-empty array of positive numbers",
      );
    }
    out.scales = scales.map((scale) => positive(scale, "a scale"));
  }
  if (limit !== undefined) {
    out.limit = integer(limit, "limit", 1, 10_000);
  }
  if (overlap !== undefined) {
    if (typeof overlap !== "number" || !(overlap >= 0 && overlap <= 1)) {
      throw new RangeError("overlap must be from 0 to 1");
    }
    out.overlap = overlap;
  }
  return out;
};

export class RasterImage implements RasterImageInterface {
  declare readonly width: number;
  declare readonly height: number;
  declare readonly scale: number;
  declare readonly frame: Rect;
  declare readonly time: number | null;
  declare readonly timestamp: number | null;

  constructor(handle: ImageHandle) {
    handles.set(this, handle);
    const frame = handle.frame;
    Object.defineProperties(this, {
      width: { value: handle.width, enumerable: true },
      height: { value: handle.height, enumerable: true },
      scale: { value: handle.scale, enumerable: true },
      frame: {
        value: Object.freeze([frame[0], frame[1], frame[2], frame[3]]),
        enumerable: true,
      },
      time: {
        value: Number.isNaN(handle.time) ? null : handle.time,
        enumerable: true,
      },
      timestamp: {
        value: Number.isNaN(handle.timestamp) ? null : handle.timestamp,
        enumerable: true,
      },
    });
    Object.freeze(this);
  }

  crop(rect: Rect): RasterImage {
    const [x, y, width, height] = rectOf(rect, "the rect");
    return new RasterImage(handleOf(this).crop(x, y, width, height));
  }

  resize(
    size: number | { readonly width?: number; readonly height?: number },
  ): RasterImage {
    const handle = handleOf(this);
    let width: number;
    let height: number;
    if (typeof size === "number") {
      positive(size, "the scale");
      width = Math.round(handle.pixelWidth * size);
      height = Math.round(handle.pixelHeight * size);
    } else {
      const { width: wanted, height: tall } = check(size, [
        "width",
        "height",
      ]) as { width?: unknown; height?: unknown };
      if (wanted === undefined && tall === undefined) {
        throw new TypeError(
          "resize takes a scale or a width or height in pixels",
        );
      }
      width =
        wanted === undefined
          ? Math.round(
              (handle.pixelWidth * positive(tall, "height")) /
                handle.pixelHeight,
            )
          : Math.round(positive(wanted, "width"));
      height =
        tall === undefined
          ? Math.round((handle.pixelHeight * width) / handle.pixelWidth)
          : Math.round(positive(tall, "height"));
    }
    width = Math.max(width, 1);
    height = Math.max(height, 1);
    if (width > largestSide || height > largestSide) {
      throw new RangeError(
        `the image would be ${width}x${height} pixels; a side is at most ${largestSide}`,
      );
    }
    return new RasterImage(handle.resize(width, height));
  }

  rotate(degrees: number): RasterImage {
    return new RasterImage(handleOf(this).rotate(finite(degrees, "degrees")));
  }

  grayscale(): RasterImage {
    return new RasterImage(handleOf(this).grayscale());
  }

  threshold(level: number | "otsu" = "otsu"): RasterImage {
    return new RasterImage(
      handleOf(this).threshold(
        level === "otsu" ? -1 : integer(level, "the level", 0, 255),
      ),
    );
  }

  mask(color: unknown, options?: { readonly tolerance?: number }): RasterImage {
    const { tolerance } = check(options, ["tolerance"]) as {
      tolerance?: unknown;
    };
    let low: Color;
    let high: Color;
    if (Array.isArray(color) && color.length === 2) {
      if (tolerance !== undefined) {
        throw new TypeError("tolerance applies to one color, not a range");
      }
      low = colorOf(color[0], "the low color");
      high = colorOf(color[1], "the high color");
    } else {
      const center = colorOf(color, "the color");
      const spread = toleranceOf(tolerance);
      low = center.map((value) => clampChannel(value - spread)) as Color;
      high = center.map((value) => clampChannel(value + spread)) as Color;
    }
    return new RasterImage(
      handleOf(this).mask(low[0], low[1], low[2], high[0], high[1], high[2]),
    );
  }

  invert(): RasterImage {
    return new RasterImage(handleOf(this).invert());
  }

  dilate(radius = 1): RasterImage {
    return new RasterImage(
      handleOf(this).dilate(integer(radius, "the radius", 1, 64)),
    );
  }

  erode(radius = 1): RasterImage {
    return new RasterImage(
      handleOf(this).erode(integer(radius, "the radius", 1, 64)),
    );
  }

  async encode(
    format: ImageFormat = "png",
    options?: Options & { readonly quality?: number },
  ): Promise<Uint8Array> {
    const { quality } = optionsOf(options, ["quality"]);
    const result = await run<{ bytes: number }>(
      "encode",
      { format: formatOf(format), quality: qualityOf(quality) },
      [handleOf(this)],
      signalOf(options),
    );
    return bytesOf(result.bytes);
  }

  async save(
    file: unknown,
    options?: Options & {
      readonly format?: ImageFormat;
      readonly quality?: number;
    },
  ) {
    const { format, quality } = optionsOf(options, ["format", "quality"]);
    const handle = fileOf(file, "the file");
    const extension = handle.name
      .slice(handle.name.lastIndexOf(".") + 1)
      .toLowerCase();
    const chosen =
      format === undefined
        ? ((
            { png: "png", jpg: "jpeg", jpeg: "jpeg", heic: "heic" } as Record<
              string,
              string
            >
          )[extension] ?? "png")
        : formatOf(format);
    await run(
      "save",
      { ...locationOf(handle), format: chosen, quality: qualityOf(quality) },
      [handleOf(this)],
      signalOf(options),
    );
    return handle;
  }

  pixel(
    point: Point,
    options?: Options,
  ): Promise<[red: number, green: number, blue: number, alpha: number]> {
    return run(
      "pixel",
      { point: pointOf(point, "the point") },
      [handleOf(this)],
      signalOf(check(options, [])),
    );
  }

  async stats(
    options?: Options & { readonly region?: Rect },
  ): Promise<ImageStats> {
    const { region } = optionsOf(options, ["region"]);
    return freeze(
      await run<ImageStats>(
        "stats",
        { region: region === undefined ? undefined : rectOf(region, "region") },
        [handleOf(this)],
        signalOf(options),
      ),
    );
  }

  async find(
    template: RasterImage,
    options?: Options & {
      readonly threshold?: number;
      readonly region?: Rect;
      readonly scales?: readonly number[];
    },
  ) {
    const found = await run<TemplateMatch[]>(
      "find",
      { ...matchOptionsOf(options, []), limit: 1 },
      [handleOf(this), handleOf(template, "the template")],
      signalOf(options),
    );
    return found.length > 0 ? freeze(found[0]) : null;
  }

  async findAll(
    template: RasterImage,
    options?: Options & {
      readonly threshold?: number;
      readonly region?: Rect;
      readonly scales?: readonly number[];
      readonly limit?: number;
      readonly overlap?: number;
    },
  ) {
    return freeze(
      await run<TemplateMatch[]>(
        "find",
        matchOptionsOf(options, ["limit", "overlap"]),
        [handleOf(this), handleOf(template, "the template")],
        signalOf(options),
      ),
    );
  }

  async matchColors(
    checks: readonly {
      readonly point: Point;
      readonly color: unknown;
      readonly tolerance?: number;
    }[],
    options?: Options,
  ): Promise<boolean[]> {
    if (!Array.isArray(checks)) {
      throw new TypeError("the checks must be an array");
    }
    const encoded = checks.map((item, index) => {
      const { point, color, tolerance } = check(item, [
        "point",
        "color",
        "tolerance",
      ]) as { point?: unknown; color?: unknown; tolerance?: unknown };
      return {
        point: pointOf(point, `check ${index + 1}'s point`),
        color: colorOf(color, `check ${index + 1}'s color`),
        tolerance: toleranceOf(tolerance),
      };
    });
    return run(
      "colors",
      { checks: encoded },
      [handleOf(this)],
      signalOf(check(options, [])),
    );
  }

  async components(options?: Options & { readonly minimumPixels?: number }) {
    const { minimumPixels } = optionsOf(options, ["minimumPixels"]);
    return freeze(
      await run<ConnectedComponent[]>(
        "components",
        {
          minimumPixels:
            minimumPixels === undefined
              ? 1
              : integer(
                  minimumPixels,
                  "minimumPixels",
                  1,
                  Number.MAX_SAFE_INTEGER,
                ),
        },
        [handleOf(this)],
        signalOf(options),
      ),
    );
  }

  async recognizeText(
    options?: TextRecognitionOptions,
  ): Promise<RecognizedTextLine[]> {
    return linesOf(
      await run<{ lines: RawLine[]; text: number }>(
        "text",
        textOptionsOf(options),
        [handleOf(this)],
        signalOf(options),
      ),
    );
  }

  async diff(
    other: RasterImage,
    options?: Options & { readonly threshold?: number },
  ) {
    const { threshold } = optionsOf(options, ["threshold"]);
    return freeze(
      await run<ImageDifference>(
        "diff",
        {
          threshold:
            threshold === undefined
              ? undefined
              : integer(threshold, "threshold", 1, 255),
        },
        [handleOf(this), handleOf(other, "the other image")],
        signalOf(options),
      ),
    );
  }

  distance(other: RasterImage, options?: Options): Promise<number> {
    return run(
      "distance",
      {},
      [handleOf(this), handleOf(other, "the other image")],
      signalOf(check(options, [])),
    );
  }

  async nearest(
    candidates: readonly RasterImage[],
    options?: Options,
  ): Promise<{ readonly index: number; readonly distance: number }> {
    if (!Array.isArray(candidates) || candidates.length === 0) {
      throw new TypeError("the candidates must be a non-empty array of Images");
    }
    const others = candidates.map((candidate, index) =>
      handleOf(candidate, `candidate ${index + 1}`),
    );
    return freeze(
      await run(
        "nearest",
        {},
        [handleOf(this), ...others],
        signalOf(check(options, [])),
      ),
    );
  }

  async detectBarcodes(options?: Options) {
    return freeze(
      await run<DetectedBarcode[]>(
        "barcodes",
        {},
        [handleOf(this)],
        signalOf(check(options, [])),
      ),
    );
  }

  toString(): string {
    const [x, y] = this.frame;
    const at =
      x === 0 && y === 0
        ? ""
        : ` at [${Math.round(x * 10) / 10}, ${Math.round(y * 10) / 10}]`;
    return `RasterImage ${Math.round(this.width * 10) / 10}x${Math.round(this.height * 10) / 10} @${Math.round(this.scale * 100) / 100}x${at}`;
  }
}

export { freeze };

export const imageResult = async (
  result: Promise<{ image: number }>,
): Promise<RasterImage> => adopt((await result).image);

export const decode = (
  data: unknown,
  options?: Options & { readonly scale?: number },
) => {
  const { scale } = optionsOf(options, ["scale"]);
  const args: Record<string, unknown> = {};
  if (scale !== undefined) {
    if (typeof scale !== "number" || !(scale > 0 && scale <= 16)) {
      throw new RangeError("scale must be above 0 and at most 16");
    }
    args.scale = scale;
  }
  const objects: object[] = [];
  if (data instanceof Uint8Array) {
    objects.push(data);
  } else {
    Object.assign(args, locationOf(fileOf(data, "the data")));
  }
  return imageResult(run("decode", args, objects, signalOf(options)));
};
