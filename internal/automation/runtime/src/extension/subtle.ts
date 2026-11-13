import {
  bufferSource as webidlBufferSource,
  domString,
  enforcedOctet,
  enforcedUnsignedLong,
  enforcedUnsignedShort,
  isObject,
} from "../shared/webidl";
import { invoke } from "./async";
import {
  algorithms,
  canonicalNames,
  createCryptoKey,
  isKey,
  slotsOf,
  upper,
  type CryptoKey,
  type KeyFormat,
  type KeySlots,
  type KeyType,
  type KeyUsage,
  type Params,
} from "./crypto";
import { base64Bytes, bytesBase64, TextDecoder, TextEncoder } from "./encoding";
import { DOMException } from "./errors";
import { native } from "./native";

type Operation =
  | "encrypt"
  | "decrypt"
  | "sign"
  | "verify"
  | "digest"
  | "generateKey"
  | "importKey"
  | "deriveBits"
  | "wrapKey"
  | "unwrapKey"
  | "get key length";

interface JsonWebKey {
  kty?: string;
  use?: string;
  key_ops?: string[];
  alg?: string;
  ext?: boolean;
  crv?: string;
  x?: string;
  y?: string;
  d?: string;
  n?: string;
  e?: string;
  p?: string;
  q?: string;
  dp?: string;
  dq?: string;
  qi?: string;
  oth?: RsaOtherPrimesInfo[];
  k?: string;
}

interface RsaOtherPrimesInfo {
  r?: string;
  d?: string;
  t?: string;
}

interface CryptoKeyPair {
  publicKey: CryptoKey;
  privateKey: CryptoKey;
}

interface Imported {
  type: KeyType;
  material: string;
  modulusLength?: number;
  publicExponent?: string;
}

const usageOrder: readonly KeyUsage[] = [
  "encrypt",
  "decrypt",
  "sign",
  "verify",
  "deriveKey",
  "deriveBits",
  "wrapKey",
  "unwrapKey",
];
const formats: readonly KeyFormat[] = ["raw", "spki", "pkcs8", "jwk"];
const curves = ["P-256", "P-384", "P-521"];
const ecdsaAlgs: Record<string, string> = {
  "P-256": "ES256",
  "P-384": "ES384",
  "P-521": "ES512",
};
const hmacBlockSizes: Record<string, number> = {
  "SHA-1": 512,
  "SHA-256": 512,
  "SHA-384": 1024,
  "SHA-512": 1024,
};
const gcmTagLengths = [32, 64, 96, 104, 112, 120, 128];

const failure = (message: string, name: string) =>
  new DOMException(message, name);

const detached = (buffer: ArrayBufferLike) =>
  (buffer as { detached?: boolean }).detached === true;
const bufferSource = (value: unknown, what: string): Uint8Array => {
  if (value instanceof ArrayBuffer) {
    return detached(value) ? new Uint8Array(0) : new Uint8Array(value.slice(0));
  }
  if (ArrayBuffer.isView(value)) {
    return detached(value.buffer)
      ? new Uint8Array(0)
      : new Uint8Array(
          value.buffer,
          value.byteOffset,
          value.byteLength,
        ).slice();
  }
  throw new TypeError(`${what} is not an ArrayBuffer or a view of one`);
};

type Member = (value: unknown, what: string) => unknown;
type Entry = readonly [
  convert: Member,
  required: boolean,
  finish?: (value: unknown) => unknown,
];
type Dictionary = readonly Readonly<Record<string, Entry>>[];

const octet: Member = enforcedOctet;
const unsignedShort: Member = enforcedUnsignedShort;
const unsignedLong: Member = enforcedUnsignedLong;
const text: Member = domString;
const bigInteger: Member = (value, what) => {
  if (!(value instanceof Uint8Array)) {
    throw new TypeError(`${what} is not a Uint8Array`);
  }
  return value.slice();
};
const algorithmIdentifier: Member = (value, what) =>
  isObject(value) ? value : domString(value, what);
const cryptoKey: Member = (value, what) => {
  if (!isKey(value)) {
    throw new TypeError(`${what} is not a CryptoKey`);
  }
  return value;
};

const bytes = (required: boolean): Entry => [
  webidlBufferSource,
  required,
  (value) => bufferSource(value, ""),
];
const hash: Entry = [
  algorithmIdentifier,
  true,
  (value) => normalize(value, "digest"),
];

const none: Dictionary = [];
const hashed: Dictionary = [{ hash }];
const rsaHashedKeyGen: Dictionary = [
  { modulusLength: [unsignedLong, true], publicExponent: [bigInteger, true] },
  { hash },
];
const ecKey: Dictionary = [{ namedCurve: [text, true] }];
const aesLength: Dictionary = [{ length: [unsignedShort, true] }];
const hmacKey: Dictionary = [{ hash, length: [unsignedLong, false] }];
const aesCbc: Dictionary = [{ iv: bytes(true) }];
const aesCtr: Dictionary = [{ counter: bytes(true), length: [octet, true] }];
const aesGcm: Dictionary = [
  { additionalData: bytes(false), iv: bytes(true), tagLength: [octet, false] },
];
const rsaOaep: Dictionary = [{ label: bytes(false) }];
const rsaPss: Dictionary = [{ saltLength: [unsignedLong, true] }];
const ecdhDerive: Dictionary = [{ public: [cryptoKey, true] }];
const hkdf: Dictionary = [{ hash, info: bytes(true), salt: bytes(true) }];
const pbkdf2: Dictionary = [
  { hash, iterations: [unsignedLong, true], salt: bytes(true) },
];

const ciphers = {
  "RSA-OAEP": rsaOaep,
  "AES-CTR": aesCtr,
  "AES-CBC": aesCbc,
  "AES-GCM": aesGcm,
};
const signers = {
  "RSASSA-PKCS1-v1_5": none,
  "RSA-PSS": rsaPss,
  ECDSA: hashed,
  Ed25519: none,
  HMAC: none,
};
const aesKeys = {
  "AES-CTR": aesLength,
  "AES-CBC": aesLength,
  "AES-GCM": aesLength,
  "AES-KW": aesLength,
};

const registry: Record<Operation, Record<string, Dictionary>> = {
  encrypt: ciphers,
  decrypt: ciphers,
  sign: signers,
  verify: signers,
  digest: { "SHA-1": none, "SHA-256": none, "SHA-384": none, "SHA-512": none },
  generateKey: {
    "RSASSA-PKCS1-v1_5": rsaHashedKeyGen,
    "RSA-PSS": rsaHashedKeyGen,
    "RSA-OAEP": rsaHashedKeyGen,
    ECDSA: ecKey,
    ECDH: ecKey,
    Ed25519: none,
    X25519: none,
    ...aesKeys,
    HMAC: hmacKey,
  },
  importKey: {
    "RSASSA-PKCS1-v1_5": hashed,
    "RSA-PSS": hashed,
    "RSA-OAEP": hashed,
    ECDSA: ecKey,
    ECDH: ecKey,
    Ed25519: none,
    X25519: none,
    "AES-CTR": none,
    "AES-CBC": none,
    "AES-GCM": none,
    "AES-KW": none,
    HMAC: hmacKey,
    HKDF: none,
    PBKDF2: none,
  },
  deriveBits: {
    ECDH: ecdhDerive,
    X25519: ecdhDerive,
    HKDF: hkdf,
    PBKDF2: pbkdf2,
  },
  wrapKey: { "AES-KW": none },
  unwrapKey: { "AES-KW": none },
  "get key length": { ...aesKeys, HMAC: hmacKey, HKDF: none, PBKDF2: none },
};

const algorithmName = (input: Record<string, unknown>): string => {
  const name = input.name;
  if (name === undefined) {
    throw new TypeError("the algorithm has no name");
  }
  return domString(name, "the algorithm name");
};

function normalize(algorithm: unknown, operation: Operation): Params {
  const input = isObject(algorithm)
    ? (algorithm as Record<string, unknown>)
    : { name: domString(algorithm, "the algorithm") };
  const requested = algorithmName(input);
  const name = canonicalNames.get(upper(requested));
  const dictionary = name === undefined ? undefined : registry[operation][name];
  if (name === undefined || dictionary === undefined) {
    throw failure(
      `${requested} does not support ${operation}`,
      "NotSupportedError",
    );
  }
  algorithmName(input);
  const params = { name } as Params & Record<string, unknown>;
  const members = dictionary.flatMap((level) =>
    Object.keys(level)
      .sort()
      .map((member) => [member, level[member]] as const),
  );
  for (const [member, [convert, required]] of members) {
    const value = input[member];
    if (value !== undefined) {
      params[member] = convert(value, `the ${name} ${member}`);
    } else if (required) {
      throw new TypeError(`${name} requires ${member}`);
    }
  }
  for (const [member, [, , finish]] of members) {
    if (finish !== undefined && params[member] !== undefined) {
      params[member] = finish(params[member]);
    }
  }
  return params;
}

function normalizeWrapping(
  algorithm: unknown,
  operation: "wrapKey" | "unwrapKey",
): Params {
  try {
    return normalize(algorithm, operation);
  } catch {
    return normalize(
      algorithm,
      operation === "wrapKey" ? "encrypt" : "decrypt",
    );
  }
}

const formatOf = (value: unknown): KeyFormat => {
  const format = domString(value, "the key format") as KeyFormat;
  if (!formats.includes(format)) {
    throw new TypeError(
      `${format} is not a key format; the formats are ${formats.join(", ")}`,
    );
  }
  return format;
};

const usagesOf = (value: unknown): KeyUsage[] => {
  if (
    value === null ||
    typeof value !== "object" ||
    !(Symbol.iterator in value)
  ) {
    throw new TypeError("the key usages are not a sequence");
  }
  const requested = new Set<string>();
  for (const usage of value as Iterable<unknown>) {
    const name = domString(usage, "the key usage");
    if (!usageOrder.includes(name as KeyUsage)) {
      throw new TypeError(
        `${name} is not a key usage; the usages are ${usageOrder.join(", ")}`,
      );
    }
    requested.add(name);
  }
  return usageOrder.filter((usage) => requested.has(usage));
};

const permit = (
  usages: readonly KeyUsage[],
  allowed: readonly KeyUsage[] = [],
  name: string,
) => {
  const invalid = usages.find((usage) => !allowed.includes(usage));
  if (invalid !== undefined) {
    throw failure(
      `${invalid} is not a usage of this ${name} key`,
      "SyntaxError",
    );
  }
};

const primeOf = (value: unknown): RsaOtherPrimesInfo => {
  if (value !== undefined && value !== null && typeof value !== "object") {
    throw new TypeError("the JWK oth entry is not a dictionary");
  }
  const input = (value ?? {}) as Record<string, unknown>;
  return Object.fromEntries(
    (["d", "r", "t"] as const)
      .filter((member) => input[member] !== undefined)
      .map((member) => [
        member,
        domString(input[member], `the JWK oth ${member}`),
      ]),
  );
};

const jwkOf = (value: unknown): JsonWebKey => {
  if (
    value instanceof ArrayBuffer ||
    ArrayBuffer.isView(value) ||
    (value !== undefined && value !== null && typeof value !== "object")
  ) {
    throw new TypeError("the key data is not a JSON Web Key");
  }
  const input = (value ?? {}) as Record<string, unknown>;
  const jwk: JsonWebKey = {};
  for (const member of [
    "alg",
    "crv",
    "d",
    "dp",
    "dq",
    "e",
    "ext",
    "k",
    "key_ops",
    "kty",
    "n",
    "oth",
    "p",
    "q",
    "qi",
    "use",
    "x",
    "y",
  ] as const) {
    const item = input[member];
    if (item === undefined) {
      continue;
    }
    if (member === "ext") {
      jwk.ext = Boolean(item);
    } else if (member === "key_ops" || member === "oth") {
      if (
        item === null ||
        typeof item !== "object" ||
        !(Symbol.iterator in item)
      ) {
        throw new TypeError(`the JWK ${member} is not a sequence`);
      }
      const entries = Array.from(item as Iterable<unknown>);
      if (member === "key_ops") {
        jwk.key_ops = entries.map((entry) =>
          domString(entry, "the JWK key_ops entry"),
        );
      } else {
        jwk.oth = entries.map(primeOf);
      }
    } else {
      jwk[member] = domString(item, `the JWK ${member}`);
    }
  }
  return jwk;
};

const checkJwk = (
  jwk: JsonWebKey,
  use: string | undefined,
  usages: readonly KeyUsage[],
  extractable: boolean,
) => {
  if (
    use !== undefined &&
    usages.length > 0 &&
    jwk.use !== undefined &&
    jwk.use !== use
  ) {
    throw failure(`the JWK use is not ${use}`, "DataError");
  }
  if (jwk.key_ops !== undefined) {
    if (new Set(jwk.key_ops).size !== jwk.key_ops.length) {
      throw failure("the JWK key_ops has duplicates", "DataError");
    }
    const missing = usages.find((usage) => !jwk.key_ops!.includes(usage));
    if (missing !== undefined) {
      throw failure(`the JWK key_ops does not include ${missing}`, "DataError");
    }
  }
  if (jwk.ext === false && extractable) {
    throw failure("the JWK is not extractable", "DataError");
  }
};

const jwkAlg = (algorithm: Params): string | undefined => {
  const bits = algorithm.hash?.name.slice(4);
  switch (algorithm.name) {
    case "AES-CTR":
    case "AES-CBC":
    case "AES-GCM":
    case "AES-KW":
      return `A${algorithm.length}${algorithm.name.slice(4)}`;
    case "HMAC":
      return `HS${bits}`;
    case "RSASSA-PKCS1-v1_5":
      return `RS${bits}`;
    case "RSA-PSS":
      return `PS${bits}`;
    case "RSA-OAEP":
      return bits === "1" ? "RSA-OAEP" : `RSA-OAEP-${bits}`;
    case "Ed25519":
      return "Ed25519";
  }
  return undefined;
};

const encodeBase64URL = (bytes: Uint8Array) =>
  base64Bytes(bytes).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");

const decodeBase64URL = (value: string, what: string): Uint8Array => {
  const trimmed = value.replace(/=+$/, "");
  if (!/^[A-Za-z0-9_-]*$/.test(trimmed) || trimmed.length % 4 === 1) {
    throw failure(`${what} is not base64url`, "DataError");
  }
  return bytesBase64(trimmed.replace(/-/g, "+").replace(/_/g, "/"));
};

const arrayBuffer = (bytes: Uint8Array): ArrayBuffer =>
  bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength
    ? (bytes.buffer as ArrayBuffer)
    : (bytes.buffer.slice(
        bytes.byteOffset,
        bytes.byteOffset + bytes.byteLength,
      ) as ArrayBuffer);

const keyOf = (value: unknown, what: string): KeySlots => {
  if (!isKey(value)) {
    throw new TypeError(`${what} is not a CryptoKey`);
  }
  return slotsOf(value);
};

const authorize = (slots: KeySlots, params: Params, usage: KeyUsage) => {
  if (params.name !== slots.algorithm.name) {
    throw failure(
      `the key is for ${slots.algorithm.name}, not ${params.name}`,
      "InvalidAccessError",
    );
  }
  if (!slots.usages.includes(usage)) {
    throw failure(`the key does not allow ${usage}`, "InvalidAccessError");
  }
};

const requireType = (slots: KeySlots, type: KeyType) => {
  if (slots.type !== type) {
    throw failure(`the key is not a ${type} key`, "InvalidAccessError");
  }
};

const wire = (params: Params): Record<string, unknown> =>
  Object.fromEntries(
    Object.entries(params)
      .filter(([, value]) => value !== undefined)
      .map(([member, value]) => [
        member,
        value instanceof Uint8Array
          ? base64Bytes(value)
          : isKey(value)
            ? slotsOf(value).material
            : member === "hash"
              ? (value as Params).name
              : value,
      ]),
  );

async function backend<T>(
  operation: string,
  name: string,
  ...args: unknown[]
): Promise<T> {
  try {
    return await invoke<T>(`crypto.${operation}`, args);
  } catch (error) {
    throw failure(error instanceof Error ? error.message : String(error), name);
  }
}

async function crypt(
  operation: "encrypt" | "decrypt",
  params: Params,
  slots: KeySlots,
  data: Uint8Array,
): Promise<Uint8Array> {
  const request = { ...params };
  switch (params.name) {
    case "RSA-OAEP":
      requireType(slots, operation === "encrypt" ? "public" : "private");
      request.hash = slots.algorithm.hash;
      break;
    case "AES-CBC":
      if (params.iv!.length !== 16) {
        throw failure("the AES-CBC iv is not 16 bytes", "OperationError");
      }
      break;
    case "AES-CTR":
      if (params.counter!.length !== 16) {
        throw failure("the AES-CTR counter is not 16 bytes", "OperationError");
      }
      if (params.length! === 0 || params.length! > 128) {
        throw failure(
          "the AES-CTR length is not between 1 and 128",
          "OperationError",
        );
      }
      break;
    case "AES-GCM":
      request.tagLength = params.tagLength ?? 128;
      if (!gcmTagLengths.includes(request.tagLength)) {
        throw failure(
          `the AES-GCM tagLength is not one of ${gcmTagLengths.join(", ")}`,
          "OperationError",
        );
      }
      if (params.iv!.length === 0) {
        throw failure("the AES-GCM iv is empty", "OperationError");
      }
      break;
  }
  return backend(
    operation,
    "OperationError",
    wire(request),
    slots.material,
    native.put(data),
  );
}

async function derive(
  params: Params,
  slots: KeySlots,
  length: number | null,
): Promise<Uint8Array> {
  switch (params.name) {
    case "ECDH":
    case "X25519": {
      requireType(slots, "private");
      const peer = slotsOf(params.public!);
      if (peer.type !== "public") {
        throw failure(
          "the public key is not a public key",
          "InvalidAccessError",
        );
      }
      if (
        peer.algorithm.name !== slots.algorithm.name ||
        peer.algorithm.namedCurve !== slots.algorithm.namedCurve
      ) {
        throw failure(
          "the public key is not for the same algorithm and curve",
          "InvalidAccessError",
        );
      }
      break;
    }
    default:
      if (length === null || length % 8 !== 0) {
        throw failure(
          `the ${params.name} length is not a multiple of 8`,
          "OperationError",
        );
      }
      if (params.name === "PBKDF2" && params.iterations === 0) {
        throw failure("the PBKDF2 iterations is 0", "OperationError");
      }
  }
  return backend(
    "derive",
    "OperationError",
    wire(params),
    slots.material,
    length,
  );
}

const secretLength = (params: Params): number => {
  if (params.name === "HMAC") {
    if (params.length === 0) {
      throw failure("the HMAC length is 0", "OperationError");
    }
    return params.length ?? hmacBlockSizes[params.hash!.name];
  }
  if (![128, 192, 256].includes(params.length!)) {
    throw failure(
      `the ${params.name} length is not 128, 192, or 256`,
      "OperationError",
    );
  }
  return params.length!;
};

const keyAlgorithm = (params: Params): Params => {
  switch (algorithms[params.name].kind) {
    case "aes":
      return { name: params.name, length: params.length };
    case "hmac":
      return { name: params.name, hash: params.hash, length: params.length };
    case "rsa":
      return {
        name: params.name,
        modulusLength: params.modulusLength,
        publicExponent: params.publicExponent,
        hash: params.hash,
      };
    case "ec":
      return { name: params.name, namedCurve: params.namedCurve };
  }
  return { name: params.name };
};

const createKey = (slots: KeySlots): CryptoKey => {
  if (slots.type !== "public" && slots.usages.length === 0) {
    throw failure(`the ${slots.type} key has no usages`, "SyntaxError");
  }
  return createCryptoKey(slots);
};

async function generate(
  params: Params,
  extractable: boolean,
  usages: KeyUsage[],
): Promise<CryptoKey | CryptoKeyPair> {
  const algorithm = algorithms[params.name];
  if (algorithm.secret !== undefined) {
    permit(usages, algorithm.secret, params.name);
    const length = secretLength(params);
    const bytes = native.random(Math.ceil(length / 8));
    if (length % 8 !== 0) {
      bytes[bytes.length - 1] &= 0xff << (8 - (length % 8));
    }
    return createKey({
      type: "secret",
      extractable,
      algorithm: keyAlgorithm({ ...params, length }),
      usages,
      material: base64Bytes(bytes),
    });
  }
  permit(usages, [...algorithm.public!, ...algorithm.private!], params.name);
  if (algorithm.kind === "ec" && !curves.includes(params.namedCurve!)) {
    throw failure(
      `the curve ${params.namedCurve} is not supported; the curves are ${curves.join(", ")}`,
      "NotSupportedError",
    );
  }
  const pair = await backend<{ private: string; public: string }>(
    "generate",
    "OperationError",
    wire(params),
  );
  return {
    publicKey: createKey({
      type: "public",
      extractable: true,
      algorithm: keyAlgorithm(params),
      usages: usages.filter((usage) => algorithm.public!.includes(usage)),
      material: pair.public,
    }),
    privateKey: createKey({
      type: "private",
      extractable,
      algorithm: keyAlgorithm(params),
      usages: usages.filter((usage) => algorithm.private!.includes(usage)),
      material: pair.private,
    }),
  };
}

async function importKeyData(
  format: KeyFormat,
  data: Uint8Array | JsonWebKey,
  params: Params,
  extractable: boolean,
  usages: KeyUsage[],
): Promise<CryptoKey> {
  const algorithm = algorithms[params.name];
  const jwk = format === "jwk" ? (data as JsonWebKey) : undefined;
  switch (algorithm.kind) {
    case "kdf":
      if (format !== "raw") {
        throw failure(
          `${params.name} keys are imported only as raw`,
          "NotSupportedError",
        );
      }
      permit(usages, algorithm.secret, params.name);
      if (extractable) {
        throw failure(`${params.name} keys are not extractable`, "SyntaxError");
      }
      return createKey({
        type: "secret",
        extractable,
        algorithm: { name: params.name },
        usages,
        material: base64Bytes(data as Uint8Array),
      });
    case "aes":
    case "hmac": {
      permit(usages, algorithm.secret, params.name);
      if (format !== "raw" && format !== "jwk") {
        throw failure(
          `${params.name} keys are imported only as raw or jwk`,
          "NotSupportedError",
        );
      }
      let bytes = data as Uint8Array;
      if (jwk !== undefined) {
        if (jwk.kty !== "oct") {
          throw failure("the JWK kty is not oct", "DataError");
        }
        if (jwk.k === undefined) {
          throw failure("the JWK has no k", "DataError");
        }
        bytes = decodeBase64URL(jwk.k, "the JWK k");
      }
      let length = bytes.length * 8;
      if (algorithm.kind === "aes" && ![128, 192, 256].includes(length)) {
        throw failure(
          `the ${params.name} key is ${length} bits, not 128, 192, or 256`,
          "DataError",
        );
      }
      if (algorithm.kind === "hmac") {
        if (length === 0) {
          throw failure("the HMAC key is empty", "DataError");
        }
        if (params.length !== undefined) {
          if (params.length > length || params.length <= length - 8) {
            throw failure(
              `the HMAC length ${params.length} does not match the ${length}-bit key`,
              "DataError",
            );
          }
          length = params.length;
        }
      }
      const keyParams = keyAlgorithm({ ...params, length });
      if (jwk !== undefined) {
        if (jwk.alg !== undefined && jwk.alg !== jwkAlg(keyParams)) {
          throw failure(`the JWK alg is not ${jwkAlg(keyParams)}`, "DataError");
        }
        checkJwk(jwk, algorithm.use, usages, extractable);
      }
      return createKey({
        type: "secret",
        extractable,
        algorithm: keyParams,
        usages,
        material: base64Bytes(bytes),
      });
    }
  }
  switch (format) {
    case "raw":
      if (algorithm.kind === "rsa") {
        throw failure("RSA keys are not imported as raw", "NotSupportedError");
      }
      permit(usages, algorithm.public, params.name);
      break;
    case "spki":
      permit(usages, algorithm.public, params.name);
      break;
    case "pkcs8":
      permit(usages, algorithm.private, params.name);
      break;
    case "jwk": {
      permit(
        usages,
        jwk!.d === undefined ? algorithm.public : algorithm.private,
        params.name,
      );
      if (jwk!.kty === undefined) {
        throw failure("the JWK has no kty", "DataError");
      }
      const expected =
        algorithm.kind === "ec"
          ? params.name === "ECDSA"
            ? ecdsaAlgs[params.namedCurve!]
            : undefined
          : jwkAlg(params);
      if (
        jwk!.alg !== undefined &&
        expected !== undefined &&
        jwk!.alg !== expected &&
        !(params.name === "Ed25519" && jwk!.alg === "EdDSA")
      ) {
        throw failure(`the JWK alg is not ${expected}`, "DataError");
      }
      checkJwk(jwk!, algorithm.use, usages, extractable);
    }
  }
  if (algorithm.kind === "ec" && !curves.includes(params.namedCurve!)) {
    throw failure(
      `the curve ${params.namedCurve} is not supported; the curves are ${curves.join(", ")}`,
      "DataError",
    );
  }
  const imported = await backend<Imported>(
    "import",
    "DataError",
    format,
    wire(params),
    jwk ?? base64Bytes(data as Uint8Array),
  );
  const keyParams = keyAlgorithm({
    ...params,
    modulusLength: imported.modulusLength,
    publicExponent:
      imported.publicExponent === undefined
        ? undefined
        : bytesBase64(imported.publicExponent),
  });
  return createKey({
    type: imported.type,
    extractable,
    algorithm: keyParams,
    usages,
    material: imported.material,
  });
}

async function exportKeyData(
  format: KeyFormat,
  slots: KeySlots,
): Promise<ArrayBuffer | JsonWebKey> {
  const { kind } = algorithms[slots.algorithm.name];
  if (kind === "kdf") {
    throw failure(
      `${slots.algorithm.name} keys cannot be exported`,
      "NotSupportedError",
    );
  }
  if (!slots.extractable) {
    throw failure("the key is not extractable", "InvalidAccessError");
  }
  const alg = jwkAlg(slots.algorithm);
  const metadata = {
    ...(alg === undefined ? {} : { alg }),
    key_ops: [...slots.usages],
    ext: slots.extractable,
  };
  if (kind === "aes" || kind === "hmac") {
    const bytes = bytesBase64(slots.material);
    switch (format) {
      case "raw":
        return arrayBuffer(bytes);
      case "jwk":
        return { kty: "oct", k: encodeBase64URL(bytes), ...metadata };
    }
    throw failure(
      `${slots.algorithm.name} keys are exported only as raw or jwk`,
      "NotSupportedError",
    );
  }
  switch (format) {
    case "raw":
      if (kind === "rsa") {
        throw failure("RSA keys are not exported as raw", "NotSupportedError");
      }
      requireType(slots, "public");
      break;
    case "spki":
      requireType(slots, "public");
      break;
    case "pkcs8":
      requireType(slots, "private");
      break;
  }
  const exported = await backend<Uint8Array | JsonWebKey>(
    "export",
    "OperationError",
    format,
    wire(slots.algorithm),
    slots.material,
    slots.type,
  );
  if (exported instanceof Uint8Array) {
    return arrayBuffer(exported);
  }
  return { ...exported, ...metadata };
}

const keyLength = (params: Params): number | null => {
  switch (algorithms[params.name].kind) {
    case "aes":
      return secretLength(params);
    case "hmac":
      if (params.length === 0) {
        throw new TypeError("the HMAC length is 0");
      }
      return params.length ?? hmacBlockSizes[params.hash!.name];
  }
  return null;
};

export async function encrypt(
  algorithm: unknown,
  key: CryptoKey,
  data: unknown,
): Promise<ArrayBuffer> {
  const slots = keyOf(key, "the key");
  const params = normalize(algorithm, "encrypt");
  const bytes = bufferSource(data, "the data");
  authorize(slots, params, "encrypt");
  return arrayBuffer(await crypt("encrypt", params, slots, bytes));
}

export async function decrypt(
  algorithm: unknown,
  key: CryptoKey,
  data: unknown,
): Promise<ArrayBuffer> {
  const slots = keyOf(key, "the key");
  const params = normalize(algorithm, "decrypt");
  const bytes = bufferSource(data, "the data");
  authorize(slots, params, "decrypt");
  return arrayBuffer(await crypt("decrypt", params, slots, bytes));
}

export async function sign(
  algorithm: unknown,
  key: CryptoKey,
  data: unknown,
): Promise<ArrayBuffer> {
  const slots = keyOf(key, "the key");
  const params = normalize(algorithm, "sign");
  const bytes = bufferSource(data, "the data");
  authorize(slots, params, "sign");
  requireType(slots, params.name === "HMAC" ? "secret" : "private");
  const request = wire({
    ...params,
    hash: params.hash ?? slots.algorithm.hash,
  });
  return arrayBuffer(
    await backend(
      "sign",
      "OperationError",
      request,
      slots.material,
      native.put(bytes),
    ),
  );
}

export async function verify(
  algorithm: unknown,
  key: CryptoKey,
  signature: unknown,
  data: unknown,
): Promise<boolean> {
  const slots = keyOf(key, "the key");
  const params = normalize(algorithm, "verify");
  const signatureBytes = bufferSource(signature, "the signature");
  const bytes = bufferSource(data, "the data");
  authorize(slots, params, "verify");
  requireType(slots, params.name === "HMAC" ? "secret" : "public");
  const request = wire({
    ...params,
    hash: params.hash ?? slots.algorithm.hash,
  });
  return backend(
    "verify",
    "OperationError",
    request,
    slots.material,
    native.put(bytes),
    base64Bytes(signatureBytes),
  );
}

export async function digest(
  algorithm: unknown,
  data: unknown,
): Promise<ArrayBuffer> {
  const params = normalize(algorithm, "digest");
  const bytes = bufferSource(data, "the data");
  return arrayBuffer(native.digest(params.name, bytes));
}

export async function generateKey(
  algorithm: unknown,
  extractable: boolean,
  keyUsages: Iterable<KeyUsage>,
): Promise<CryptoKey | CryptoKeyPair> {
  const exportable = Boolean(extractable);
  const usages = usagesOf(keyUsages);
  return generate(normalize(algorithm, "generateKey"), exportable, usages);
}

export async function deriveKey(
  algorithm: unknown,
  baseKey: CryptoKey,
  derivedKeyType: unknown,
  extractable: boolean,
  keyUsages: Iterable<KeyUsage>,
): Promise<CryptoKey> {
  const slots = keyOf(baseKey, "the base key");
  const exportable = Boolean(extractable);
  const usages = usagesOf(keyUsages);
  const params = normalize(algorithm, "deriveBits");
  const importParams = normalize(derivedKeyType, "importKey");
  const lengthParams = normalize(derivedKeyType, "get key length");
  authorize(slots, params, "deriveKey");
  const secret = await derive(params, slots, keyLength(lengthParams));
  return importKeyData("raw", secret, importParams, exportable, usages);
}

export async function deriveBits(
  algorithm: unknown,
  baseKey: CryptoKey,
  length: number | null,
): Promise<ArrayBuffer> {
  const slots = keyOf(baseKey, "the base key");
  const bits =
    length === null || length === undefined ? null : Number(length) >>> 0;
  const params = normalize(algorithm, "deriveBits");
  authorize(slots, params, "deriveBits");
  return arrayBuffer(await derive(params, slots, bits));
}

export async function importKey(
  format: KeyFormat,
  keyData: unknown,
  algorithm: unknown,
  extractable: boolean,
  keyUsages: Iterable<KeyUsage>,
): Promise<CryptoKey> {
  const keyFormat = formatOf(format);
  const exportable = Boolean(extractable);
  const usages = usagesOf(keyUsages);
  const params = normalize(algorithm, "importKey");
  const data =
    keyFormat === "jwk"
      ? jwkOf(keyData)
      : bufferSource(keyData, "the key data");
  return importKeyData(keyFormat, data, params, exportable, usages);
}

export async function exportKey(
  format: KeyFormat,
  key: CryptoKey,
): Promise<ArrayBuffer | JsonWebKey> {
  const keyFormat = formatOf(format);
  return exportKeyData(keyFormat, keyOf(key, "the key"));
}

export async function wrapKey(
  format: KeyFormat,
  key: CryptoKey,
  wrappingKey: CryptoKey,
  wrapAlgorithm: unknown,
): Promise<ArrayBuffer> {
  const keyFormat = formatOf(format);
  const slots = keyOf(key, "the key");
  const wrapping = keyOf(wrappingKey, "the wrapping key");
  const params = normalizeWrapping(wrapAlgorithm, "wrapKey");
  authorize(wrapping, params, "wrapKey");
  const exported = await exportKeyData(keyFormat, slots);
  let bytes: Uint8Array;
  if (exported instanceof ArrayBuffer) {
    bytes = new Uint8Array(exported);
  } else {
    const json = JSON.stringify(exported);
    bytes = new TextEncoder().encode(
      params.name === "AES-KW"
        ? json.padEnd(Math.ceil(json.length / 8) * 8, " ")
        : json,
    );
  }
  return arrayBuffer(await crypt("encrypt", params, wrapping, bytes));
}

export async function unwrapKey(
  format: KeyFormat,
  wrappedKey: unknown,
  unwrappingKey: CryptoKey,
  unwrapAlgorithm: unknown,
  unwrappedKeyAlgorithm: unknown,
  extractable: boolean,
  keyUsages: Iterable<KeyUsage>,
): Promise<CryptoKey> {
  const keyFormat = formatOf(format);
  const unwrapping = keyOf(unwrappingKey, "the unwrapping key");
  const exportable = Boolean(extractable);
  const usages = usagesOf(keyUsages);
  const params = normalizeWrapping(unwrapAlgorithm, "unwrapKey");
  const keyParams = normalize(unwrappedKeyAlgorithm, "importKey");
  const wrapped = bufferSource(wrappedKey, "the wrapped key");
  authorize(unwrapping, params, "unwrapKey");
  const bytes = await crypt("decrypt", params, unwrapping, wrapped);
  let data: Uint8Array | JsonWebKey = bytes;
  if (keyFormat === "jwk") {
    let parsed: unknown;
    try {
      parsed = JSON.parse(
        new TextDecoder("utf-8", { fatal: true }).decode(bytes),
      );
    } catch {
      throw failure("the unwrapped key is not JSON", "DataError");
    }
    data = jwkOf(parsed);
    if (data.kty === undefined) {
      throw failure("the unwrapped JWK has no kty", "DataError");
    }
  }
  return importKeyData(keyFormat, data, keyParams, exportable, usages);
}
