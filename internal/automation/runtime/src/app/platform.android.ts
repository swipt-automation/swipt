import { requireNativeModule } from "expo";
import { createElement, type ComponentType } from "react";

const { packageName } = requireNativeModule<{ packageName: string }>(
  "SwiptHost",
);

const rewriteURI = (uri: string): string => {
  if (uri.startsWith("file://")) {
    return `content://${packageName}.swiplet?path=${encodeURIComponent(
      decodeURIComponent(uri.slice(7)),
    )}`;
  }
  if (uri.startsWith("content://") || uri.startsWith("android.resource://")) {
    return "swiplet-blocked://";
  }
  return uri;
};

const rewriteSource = (source: unknown): unknown => {
  if (Array.isArray(source)) {
    return source.map(rewriteSource);
  }
  if (source !== null && typeof source === "object" && "uri" in source) {
    const uri = (source as { uri?: unknown }).uri;
    if (typeof uri === "string") {
      return { ...source, uri: rewriteURI(uri) };
    }
  }
  return source;
};

const rewriteSrcSet = (srcSet: string): string =>
  srcSet
    .split(",")
    .map((entry) => {
      const trimmed = entry.trim();
      if (trimmed === "") return trimmed;
      const [uri, ...rest] = trimmed.split(/\s+/);
      return [rewriteURI(uri!), ...rest].join(" ");
    })
    .join(", ");

const rewriteImageProps = (
  props: Record<string, unknown>,
): Record<string, unknown> => {
  const next: Record<string, unknown> = { ...props };
  if ("source" in next) next.source = rewriteSource(next.source);
  if ("defaultSource" in next) {
    next.defaultSource = rewriteSource(next.defaultSource);
  }
  if ("loadingIndicatorSource" in next) {
    next.loadingIndicatorSource = rewriteSource(next.loadingIndicatorSource);
  }
  if (typeof next.src === "string") next.src = rewriteURI(next.src);
  if (typeof next.srcSet === "string") next.srcSet = rewriteSrcSet(next.srcSet);
  return next;
};

interface ReactNativeImageStatic {
  resolveAssetSource(source: unknown): unknown;
  getSize(
    url: string,
    success?: (width: number, height: number) => void,
    failure?: (error: unknown) => void,
  ): unknown;
  getSizeWithHeaders(
    url: string,
    headers: Record<string, string>,
    success?: (width: number, height: number) => void,
    failure?: (error: unknown) => void,
  ): unknown;
  prefetch(url: string, callback?: (requestId: number) => void): unknown;
}

const BaseReactNative = require("react-native") as Record<string, unknown> & {
  Image: ComponentType<Record<string, unknown>> & ReactNativeImageStatic;
  ImageBackground: ComponentType<Record<string, unknown>>;
};

const RawImage = BaseReactNative.Image;
const originalResolve = RawImage.resolveAssetSource.bind(RawImage);
RawImage.resolveAssetSource = (source) =>
  rewriteSource(originalResolve(source));

const originalGetSize = RawImage.getSize.bind(RawImage);
RawImage.getSize = (url, success, failure) =>
  originalGetSize(rewriteURI(url), success, failure);

const originalGetSizeWithHeaders = RawImage.getSizeWithHeaders.bind(RawImage);
RawImage.getSizeWithHeaders = (url, headers, success, failure) =>
  originalGetSizeWithHeaders(rewriteURI(url), headers, success, failure);

const originalPrefetch = RawImage.prefetch.bind(RawImage);
RawImage.prefetch = (url, callback) =>
  originalPrefetch(rewriteURI(url), callback);

const SandboxedImage = Object.assign(
  (props: Record<string, unknown>) =>
    createElement(RawImage, rewriteImageProps(props)),
  RawImage,
);

const RawImageBackground = BaseReactNative.ImageBackground;
const SandboxedImageBackground = (props: Record<string, unknown>) =>
  createElement(RawImageBackground, rewriteImageProps(props));

let reactNative: Record<string, unknown> | undefined;

const getReactNative = (): Record<string, unknown> => {
  if (reactNative === undefined) {
    const target: Record<string, unknown> = {};
    for (const key of Reflect.ownKeys(BaseReactNative)) {
      if (key === "Image") {
        Object.defineProperty(target, key, {
          value: SandboxedImage,
          enumerable: true,
        });
      } else if (key === "ImageBackground") {
        Object.defineProperty(target, key, {
          value: SandboxedImageBackground,
          enumerable: true,
        });
      } else {
        const descriptor = Object.getOwnPropertyDescriptor(
          BaseReactNative,
          key,
        );
        if (descriptor !== undefined) {
          Object.defineProperty(target, key, descriptor);
        }
      }
    }
    reactNative = target;
  }
  return reactNative;
};

export const platformModules: Record<string, () => unknown> = {
  "react-native": getReactNative,
  "@expo/ui/jetpack-compose": () => require("@expo/ui/jetpack-compose"),
  "@expo/ui/jetpack-compose/modifiers": () =>
    require("@expo/ui/jetpack-compose/modifiers"),
};
