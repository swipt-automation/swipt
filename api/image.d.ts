import type { CallOptions, Point, Quad, Rect } from "./index";
import type { FileHandle } from "./file-system";

export type Color =
  `#${string}` | readonly [red: number, green: number, blue: number];
export type ImageFormat = "png" | "jpeg" | "heic";

export interface TemplateMatch {
  readonly frame: Rect;
  readonly center: Point;
  readonly score: number;
  readonly scale: number;
}

export interface TemplateMatchOptions extends CallOptions {
  readonly threshold?: number;
  readonly region?: Rect;
  readonly scales?: readonly number[];
}

export interface ColorCheck {
  readonly point: Point;
  readonly color: Color;
  readonly tolerance?: number;
}

export interface ConnectedComponent {
  readonly frame: Rect;
  readonly pixels: number;
  readonly centroid: Point;
}

export interface ImageStats {
  readonly mean: readonly number[];
  readonly min: readonly number[];
  readonly max: readonly number[];
  readonly deviation: readonly number[];
}

export interface TextRecognitionOptions extends CallOptions {
  readonly region?: Rect;
  readonly languages?: readonly string[];
  readonly detectLanguage?: boolean;
  readonly level?: "fast" | "accurate";
  readonly words?: readonly string[];
  readonly correction?: boolean;
  readonly minimumHeight?: number;
}

export interface RecognizedTextBox {
  readonly quad: Quad;
  readonly frame: Rect;
  readonly center: Point;
}

export interface RecognizedTextCandidate {
  readonly text: string;
  readonly confidence: number;
  boundingBox(start: number, end: number): RecognizedTextBox | null;
}

export interface RecognizedTextLine
  extends RecognizedTextCandidate, RecognizedTextBox {
  readonly candidates: readonly RecognizedTextCandidate[];
}

export interface ImageDifference {
  readonly ratio: number;
  readonly frames: readonly Rect[];
}

export interface DetectedBarcode {
  readonly payload: string | null;
  readonly symbology: string;
  readonly quad: Quad;
  readonly frame: Rect;
  readonly center: Point;
}

/**
 * An image covers frame in the coordinates of where it came from, with scale pixels to one of their units: a
 * screenshot or a frame of a screen spans the screen in the coordinates that input takes, and an image that decode
 * answers starts at [0, 0]. Every Point and Rect that an image takes or answers is in these coordinates, so a match in
 * a screenshot can be tapped as it is. An image made from another keeps these coordinates: crop answers the part of
 * frame that the rect covers, in its place; resize changes the pixels and the scale, not frame; rotate turns the image
 * about the center of frame. encode and save keep the pixels and the scale, not the place of frame.
 */
export interface RasterImage {
  /** The width of frame. */
  readonly width: number;
  /** The height of frame. */
  readonly height: number;
  readonly scale: number;
  readonly frame: Rect;
  /** For a frame of a recording, the milliseconds after the recording started; otherwise null. */
  readonly time: number | null;
  /** The Unix time in milliseconds when the screen showed the image; null for an image that decode answers. */
  readonly timestamp: number | null;

  crop(rect: Rect): RasterImage;
  /** Multiplies the pixels on each side by the scale. */
  resize(scale: number): RasterImage;
  /** In pixels; one side alone keeps the aspect ratio. */
  resize(size: {
    readonly width?: number;
    readonly height?: number;
  }): RasterImage;
  /** Clockwise. */
  rotate(degrees: number): RasterImage;
  grayscale(): RasterImage;
  threshold(level?: number | "otsu"): RasterImage;
  mask(color: Color, options?: { readonly tolerance?: number }): RasterImage;
  mask(range: readonly [low: Color, high: Color]): RasterImage;
  invert(): RasterImage;
  dilate(radius?: number): RasterImage;
  erode(radius?: number): RasterImage;

  /** quality, from 0 to 1, applies to jpeg and heic. */
  encode(
    format?: "png" | "jpeg",
    options?: CallOptions & { readonly quality?: number },
  ): Promise<Uint8Array>;
  /** @available iOS */
  encode(
    format: "heic",
    options?: CallOptions & { readonly quality?: number },
  ): Promise<Uint8Array>;
  save(
    file: FileHandle,
    options?: CallOptions & {
      readonly format?: "png" | "jpeg";
      readonly quality?: number;
    },
  ): Promise<FileHandle>;
  /** @available iOS */
  save(
    file: FileHandle,
    options: CallOptions & {
      readonly format: "heic";
      readonly quality?: number;
    },
  ): Promise<FileHandle>;
  pixel(
    point: Point,
    options?: CallOptions,
  ): Promise<[red: number, green: number, blue: number, alpha: number]>;
  stats(
    options?: CallOptions & { readonly region?: Rect },
  ): Promise<ImageStats>;

  find(
    template: RasterImage,
    options?: TemplateMatchOptions,
  ): Promise<TemplateMatch | null>;
  findAll(
    template: RasterImage,
    options?: TemplateMatchOptions & {
      readonly limit?: number;
      readonly overlap?: number;
    },
  ): Promise<TemplateMatch[]>;
  matchColors(
    checks: readonly ColorCheck[],
    options?: CallOptions,
  ): Promise<boolean[]>;
  components(
    options?: CallOptions & { readonly minimumPixels?: number },
  ): Promise<ConnectedComponent[]>;
  /** @available iOS */
  recognizeText(
    options?: TextRecognitionOptions,
  ): Promise<RecognizedTextLine[]>;
  diff(
    other: RasterImage,
    options?: CallOptions & { readonly threshold?: number },
  ): Promise<ImageDifference>;
  /** @available iOS */
  distance(other: RasterImage, options?: CallOptions): Promise<number>;
  /** @available iOS */
  nearest(
    candidates: readonly RasterImage[],
    options?: CallOptions,
  ): Promise<{ readonly index: number; readonly distance: number }>;
  /** @available iOS */
  detectBarcodes(options?: CallOptions): Promise<DetectedBarcode[]>;
}

/** scale, from above 0 to 16, is the pixels to a unit; without it, the @2x-style suffix of the file name or the pixel density of the image from 72 to 1152 dpi gives it, and otherwise it is 1. */
export declare function decode(
  data: Uint8Array | FileHandle,
  options?: CallOptions & { readonly scale?: number },
): Promise<RasterImage>;
