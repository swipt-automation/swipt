import type { Table } from "./encoding-tables";
import { labels, type Encoding } from "./encodings";
import {
  allowSharedBufferSource,
  boolean,
  brand,
  defineInterface,
  dictionary,
  domString,
  optional,
  type AllowSharedBufferSource,
} from "./webidl";

export type { Encoding };

export type EncodingTables = typeof import("./encoding-tables");

export type NativeDecode = (
  bytes: Uint8Array,
  fatal: boolean,
) => string | number;

interface Decoder {
  readonly restored: readonly number[];
  decode(bytes: Uint8Array, flush: boolean, fatal: boolean): string | undefined;
}

let labelEncodings: Map<string, Encoding> | undefined;

export const encodingOf = (label: string): Encoding | undefined => {
  if (label === "utf-8") {
    return "utf-8";
  }
  if (labelEncodings === undefined) {
    labelEncodings = new Map();
    for (const [encoding, names] of Object.entries(labels) as [
      Encoding,
      readonly string[],
    ][]) {
      for (const name of names) {
        labelEncodings.set(name, encoding);
      }
    }
  }
  return labelEncodings.get(
    label
      .replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, "")
      .replace(/[A-Z]+/g, (upper) => upper.toLowerCase()),
  );
};

export const bytesOf = (input: unknown): Uint8Array => {
  if (input === undefined) {
    return new Uint8Array(0);
  }
  if (input instanceof Uint8Array) {
    return input;
  }
  if (ArrayBuffer.isView(input)) {
    return new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
  }
  if (input instanceof ArrayBuffer) {
    return new Uint8Array(input);
  }
  throw new TypeError("the input is not an ArrayBuffer or a view of one");
};

const sharedArrayBuffer =
  typeof SharedArrayBuffer === "function" ? SharedArrayBuffer : undefined;

export const allowSharedBytesOf = (input: unknown): Uint8Array => {
  if (input instanceof Uint8Array) {
    return input;
  }
  if (ArrayBuffer.isView(input)) {
    return new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
  }
  if (
    input instanceof ArrayBuffer ||
    (sharedArrayBuffer !== undefined && input instanceof sharedArrayBuffer)
  ) {
    return new Uint8Array(input);
  }
  throw new TypeError(
    "the input is not an ArrayBuffer, a SharedArrayBuffer, or a view of one",
  );
};

const stringOf = (units: Uint16Array, count: number): string => {
  let out = "";
  for (let start = 0; start < count; start += 0x2000) {
    out += String.fromCharCode.apply(
      null,
      units.subarray(
        start,
        Math.min(count, start + 0x2000),
      ) as unknown as number[],
    );
  }
  return out;
};

const digitOf = (code: number): number =>
  code - 0x21 - (code > 0x22 ? 1 : 0) - (code > 0x5c ? 1 : 0);

const expanded = new Map<Table, Uint16Array | Uint32Array>();

const expand = (table: Table, wide = false): Uint16Array | Uint32Array => {
  let out = expanded.get(table);
  if (out !== undefined) {
    return out;
  }
  out = wide ? new Uint32Array(table.length) : new Uint16Array(table.length);
  const { data } = table;
  let position = 0;
  let value = 0;
  for (let index = 0; index < data.length;) {
    let digit = digitOf(data.charCodeAt(index++));
    let marker = 0;
    if (digit >= 90) {
      marker = digit;
      digit = digitOf(data.charCodeAt(index++));
    }
    let number = 0;
    let scale = 1;
    while (digit >= 45) {
      number += (digit - 45) * scale;
      scale *= 45;
      digit = digitOf(data.charCodeAt(index++));
    }
    number += digit * scale;
    if (marker === 90) {
      position += number + 1;
    } else if (marker === 91) {
      for (let count = 0; count <= number; count++) {
        out[position++] = ++value;
      }
    } else {
      value += number & 1 ? -(number + 1) / 2 : number / 2;
      out[position++] = value;
    }
  }
  expanded.set(table, out);
  return out;
};

const gb18030CodePoint = (
  ranges: EncodingTables["gb18030Ranges"],
  pointer: number,
): number => {
  if ((pointer > 39419 && pointer < 189000) || pointer > 1237575) {
    return 0;
  }
  if (pointer === 7457) {
    return 0xe7c7;
  }
  const pointers = expand(ranges.pointers, true);
  let low = 0;
  let high = pointers.length - 1;
  while (low < high) {
    const middle = (low + high + 1) >> 1;
    if (pointers[middle] <= pointer) {
      low = middle;
    } else {
      high = middle - 1;
    }
  }
  return expand(ranges.codePoints, true)[low] + pointer - pointers[low];
};

class SingleByteDecoder implements Decoder {
  readonly restored: readonly number[] = [];
  readonly #table: Uint16Array | Uint32Array;

  constructor(table: Uint16Array | Uint32Array) {
    this.#table = table;
  }

  decode(
    bytes: Uint8Array,
    _flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const table = this.#table;
    const units = new Uint16Array(bytes.length);
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index];
      if (byte < 0x80) {
        units[index] = byte;
        continue;
      }
      const point = table[byte - 0x80];
      if (point === 0) {
        if (fatal) {
          return undefined;
        }
        units[index] = 0xfffd;
      } else {
        units[index] = point;
      }
    }
    return stringOf(units, bytes.length);
  }
}

const userDefined = Uint16Array.from(
  { length: 0x80 },
  (_, offset) => 0xf780 + offset,
);

class GB18030Decoder implements Decoder {
  readonly #index: Uint16Array | Uint32Array;
  readonly #ranges: EncodingTables["gb18030Ranges"];
  #first = 0;
  #second = 0;
  #third = 0;
  restored: readonly number[] = [];

  constructor(tables: EncodingTables) {
    this.#index = expand(tables.multiByte.gb18030);
    this.#ranges = tables.gb18030Ranges;
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const gb18030 = this.#index;
    const units = new Uint16Array(bytes.length + 8);
    let count = 0;
    let first = this.#first;
    let second = this.#second;
    let third = this.#third;
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index];
      if (third !== 0) {
        if (byte < 0x30 || byte > 0x39) {
          if (fatal) {
            return this.#fail([second, third, byte]);
          }
          units[count++] = 0xfffd;
          units[count++] = second;
          first = third;
          second = third = 0;
          index--;
          continue;
        }
        let point = gb18030CodePoint(
          this.#ranges,
          (first - 0x81) * 12600 +
            (second - 0x30) * 1260 +
            (third - 0x81) * 10 +
            byte -
            0x30,
        );
        first = second = third = 0;
        if (point === 0) {
          if (fatal) {
            return this.#fail();
          }
          units[count++] = 0xfffd;
        } else if (point > 0xffff) {
          point -= 0x10000;
          units[count++] = 0xd800 + (point >> 10);
          units[count++] = 0xdc00 + (point & 0x3ff);
        } else {
          units[count++] = point;
        }
        continue;
      }
      if (second !== 0) {
        if (byte >= 0x81 && byte <= 0xfe) {
          third = byte;
          continue;
        }
        if (fatal) {
          return this.#fail([second, byte]);
        }
        units[count++] = 0xfffd;
        units[count++] = second;
        first = second = 0;
        index--;
        continue;
      }
      if (first !== 0) {
        if (byte >= 0x30 && byte <= 0x39) {
          second = byte;
          continue;
        }
        const leading = first;
        first = 0;
        if ((byte >= 0x40 && byte <= 0x7e) || (byte >= 0x80 && byte <= 0xfe)) {
          const point =
            gb18030[
              (leading - 0x81) * 190 + byte - (byte < 0x7f ? 0x40 : 0x41)
            ];
          if (point !== 0) {
            units[count++] = point;
            continue;
          }
        }
        if (fatal) {
          return this.#fail(byte < 0x80 ? [byte] : []);
        }
        units[count++] = 0xfffd;
        if (byte < 0x80) {
          index--;
        }
        continue;
      }
      if (byte < 0x80) {
        units[count++] = byte;
      } else if (byte === 0x80) {
        units[count++] = 0x20ac;
      } else if (byte <= 0xfe) {
        first = byte;
      } else if (fatal) {
        return this.#fail();
      } else {
        units[count++] = 0xfffd;
      }
    }
    if (flush && (first !== 0 || second !== 0 || third !== 0)) {
      if (fatal) {
        return this.#fail();
      }
      units[count++] = 0xfffd;
    }
    this.#first = first;
    this.#second = second;
    this.#third = third;
    return stringOf(units, count);
  }

  #fail(restored: readonly number[] = []): undefined {
    this.#first = this.#second = this.#third = 0;
    this.restored = restored;
    return undefined;
  }
}

class Big5Decoder implements Decoder {
  readonly #index: Uint16Array | Uint32Array;
  #leading = 0;
  restored: readonly number[] = [];

  constructor(tables: EncodingTables) {
    this.#index = expand(tables.multiByte.big5, true);
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const big5 = this.#index;
    const units = new Uint16Array(bytes.length + 8);
    let count = 0;
    let leading = this.#leading;
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index];
      if (leading !== 0) {
        const lead = leading;
        leading = 0;
        if ((byte >= 0x40 && byte <= 0x7e) || (byte >= 0xa1 && byte <= 0xfe)) {
          const pointer =
            (lead - 0x81) * 157 + byte - (byte < 0x7f ? 0x40 : 0x62);
          if (
            pointer === 1133 ||
            pointer === 1135 ||
            pointer === 1164 ||
            pointer === 1166
          ) {
            units[count++] = pointer < 1164 ? 0xca : 0xea;
            units[count++] =
              pointer === 1133 || pointer === 1164 ? 0x304 : 0x30c;
            continue;
          }
          let point = big5[pointer];
          if (point > 0xffff) {
            point -= 0x10000;
            units[count++] = 0xd800 + (point >> 10);
            units[count++] = 0xdc00 + (point & 0x3ff);
            continue;
          }
          if (point !== 0) {
            units[count++] = point;
            continue;
          }
        }
        if (fatal) {
          return this.#fail(byte < 0x80 ? [byte] : []);
        }
        units[count++] = 0xfffd;
        if (byte < 0x80) {
          index--;
        }
        continue;
      }
      if (byte < 0x80) {
        units[count++] = byte;
      } else if (byte >= 0x81 && byte <= 0xfe) {
        leading = byte;
      } else if (fatal) {
        return this.#fail();
      } else {
        units[count++] = 0xfffd;
      }
    }
    if (flush && leading !== 0) {
      if (fatal) {
        return this.#fail();
      }
      units[count++] = 0xfffd;
    }
    this.#leading = leading;
    return stringOf(units, count);
  }

  #fail(restored: readonly number[] = []): undefined {
    this.#leading = 0;
    this.restored = restored;
    return undefined;
  }
}

class EUCJPDecoder implements Decoder {
  readonly #tables: EncodingTables;
  readonly #jis0208: Uint16Array | Uint32Array;
  #jis0212: Uint16Array | Uint32Array | undefined;
  #leading = 0;
  #extended = false;
  restored: readonly number[] = [];

  constructor(tables: EncodingTables) {
    this.#tables = tables;
    this.#jis0208 = expand(tables.multiByte.jis0208);
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const units = new Uint16Array(bytes.length + 8);
    let count = 0;
    let leading = this.#leading;
    let extended = this.#extended;
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index];
      if (leading === 0x8e && byte >= 0xa1 && byte <= 0xdf) {
        leading = 0;
        units[count++] = 0xff61 - 0xa1 + byte;
        continue;
      }
      if (leading === 0x8f && byte >= 0xa1 && byte <= 0xfe) {
        extended = true;
        leading = byte;
        continue;
      }
      if (leading !== 0) {
        const lead = leading;
        leading = 0;
        let point = 0;
        if (lead >= 0xa1 && lead <= 0xfe && byte >= 0xa1 && byte <= 0xfe) {
          const table = extended
            ? (this.#jis0212 ??= expand(this.#tables.multiByte.jis0212))
            : this.#jis0208;
          point = table[(lead - 0xa1) * 94 + byte - 0xa1];
        }
        extended = false;
        if (point !== 0) {
          units[count++] = point;
          continue;
        }
        if (fatal) {
          return this.#fail(byte < 0x80 ? [byte] : []);
        }
        units[count++] = 0xfffd;
        if (byte < 0x80) {
          index--;
        }
        continue;
      }
      if (byte < 0x80) {
        units[count++] = byte;
      } else if (
        byte === 0x8e ||
        byte === 0x8f ||
        (byte >= 0xa1 && byte <= 0xfe)
      ) {
        leading = byte;
      } else if (fatal) {
        return this.#fail();
      } else {
        units[count++] = 0xfffd;
      }
    }
    if (flush && leading !== 0) {
      if (fatal) {
        return this.#fail();
      }
      units[count++] = 0xfffd;
    }
    this.#leading = leading;
    this.#extended = extended;
    return stringOf(units, count);
  }

  #fail(restored: readonly number[] = []): undefined {
    this.#leading = 0;
    this.#extended = false;
    this.restored = restored;
    return undefined;
  }
}

const ascii = 0;
const roman = 1;
const katakana = 2;
const leadingByte = 3;
const trailingByte = 4;
const escapeStart = 5;
const escapeSequence = 6;

class ISO2022JPDecoder implements Decoder {
  readonly #jis0208: Uint16Array | Uint32Array;
  #state = ascii;
  #outputState = ascii;
  #leading = 0;
  #output = false;
  restored: readonly number[] = [];

  constructor(tables: EncodingTables) {
    this.#jis0208 = expand(tables.multiByte.jis0208);
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const end = -1;
    const proceed = -1;
    const error = -2;
    const finished = -3;
    const units = new Uint16Array(bytes.length + 8);
    let count = 0;
    let state = this.#state;
    let outputState = this.#outputState;
    let leading = this.#leading;
    let output = this.#output;
    const restored: number[] = [];
    let index = 0;
    for (;;) {
      let byte: number;
      if (restored.length > 0) {
        byte = restored.shift()!;
      } else if (index < bytes.length) {
        byte = bytes[index++];
      } else if (flush) {
        byte = end;
      } else {
        break;
      }
      let result: number;
      switch (state) {
        case ascii:
        case roman:
        case katakana:
        case leadingByte:
          if (byte === 0x1b) {
            state = escapeStart;
            result = proceed;
            break;
          }
          if (byte === end) {
            result = finished;
            break;
          }
          output = false;
          if (state === leadingByte) {
            if (byte >= 0x21 && byte <= 0x7e) {
              leading = byte;
              state = trailingByte;
              result = proceed;
            } else {
              result = error;
            }
          } else if (state === katakana) {
            result =
              byte >= 0x21 && byte <= 0x5f ? 0xff61 - 0x21 + byte : error;
          } else if (byte > 0x7f || byte === 0x0e || byte === 0x0f) {
            result = error;
          } else if (state === roman && byte === 0x5c) {
            result = 0xa5;
          } else if (state === roman && byte === 0x7e) {
            result = 0x203e;
          } else {
            result = byte;
          }
          break;
        case trailingByte:
          if (byte === 0x1b) {
            state = escapeStart;
            result = error;
            break;
          }
          state = leadingByte;
          result =
            byte >= 0x21 && byte <= 0x7e
              ? this.#jis0208[(leading - 0x21) * 94 + byte - 0x21] || error
              : error;
          break;
        case escapeStart:
          if (byte === 0x24 || byte === 0x28) {
            leading = byte;
            state = escapeSequence;
            result = proceed;
            break;
          }
          if (byte !== end) {
            restored.unshift(byte);
          }
          output = false;
          state = outputState;
          result = error;
          break;
        default: {
          const lead = leading;
          leading = 0;
          let next: number | undefined;
          if (lead === 0x28 && byte === 0x42) {
            next = ascii;
          } else if (lead === 0x28 && byte === 0x4a) {
            next = roman;
          } else if (lead === 0x28 && byte === 0x49) {
            next = katakana;
          } else if (lead === 0x24 && (byte === 0x40 || byte === 0x42)) {
            next = leadingByte;
          }
          if (next !== undefined) {
            state = outputState = next;
            result = output ? error : proceed;
            output = true;
            break;
          }
          if (byte === end) {
            restored.unshift(lead);
          } else {
            restored.unshift(lead, byte);
          }
          output = false;
          state = outputState;
          result = error;
          break;
        }
      }
      if (result === finished) {
        break;
      }
      if (result === error) {
        if (fatal) {
          this.#state = state;
          this.#outputState = outputState;
          this.#leading = leading;
          this.#output = output;
          this.restored = restored;
          return undefined;
        }
        units[count++] = 0xfffd;
      } else if (result >= 0) {
        units[count++] = result;
      }
    }
    this.#state = state;
    this.#outputState = outputState;
    this.#leading = leading;
    this.#output = output;
    return stringOf(units, count);
  }
}

class ShiftJISDecoder implements Decoder {
  readonly #index: Uint16Array | Uint32Array;
  #leading = 0;
  restored: readonly number[] = [];

  constructor(tables: EncodingTables) {
    this.#index = expand(tables.multiByte.jis0208);
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const jis0208 = this.#index;
    const units = new Uint16Array(bytes.length + 8);
    let count = 0;
    let leading = this.#leading;
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index];
      if (leading !== 0) {
        const lead = leading;
        leading = 0;
        if ((byte >= 0x40 && byte <= 0x7e) || (byte >= 0x80 && byte <= 0xfc)) {
          const pointer =
            (lead - (lead < 0xa0 ? 0x81 : 0xc1)) * 188 +
            byte -
            (byte < 0x7f ? 0x40 : 0x41);
          if (pointer >= 8836 && pointer <= 10715) {
            units[count++] = 0xe000 - 8836 + pointer;
            continue;
          }
          const point = jis0208[pointer];
          if (point !== 0) {
            units[count++] = point;
            continue;
          }
        }
        if (fatal) {
          return this.#fail(byte < 0x80 ? [byte] : []);
        }
        units[count++] = 0xfffd;
        if (byte < 0x80) {
          index--;
        }
        continue;
      }
      if (byte <= 0x80) {
        units[count++] = byte;
      } else if (byte >= 0xa1 && byte <= 0xdf) {
        units[count++] = 0xff61 - 0xa1 + byte;
      } else if (byte <= 0x9f || (byte >= 0xe0 && byte <= 0xfc)) {
        leading = byte;
      } else if (fatal) {
        return this.#fail();
      } else {
        units[count++] = 0xfffd;
      }
    }
    if (flush && leading !== 0) {
      if (fatal) {
        return this.#fail();
      }
      units[count++] = 0xfffd;
    }
    this.#leading = leading;
    return stringOf(units, count);
  }

  #fail(restored: readonly number[] = []): undefined {
    this.#leading = 0;
    this.restored = restored;
    return undefined;
  }
}

class EUCKRDecoder implements Decoder {
  readonly #index: Uint16Array | Uint32Array;
  #leading = 0;
  restored: readonly number[] = [];

  constructor(tables: EncodingTables) {
    this.#index = expand(tables.multiByte["euc-kr"]);
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const euckr = this.#index;
    const units = new Uint16Array(bytes.length + 8);
    let count = 0;
    let leading = this.#leading;
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index];
      if (leading !== 0) {
        const lead = leading;
        leading = 0;
        if (byte >= 0x41 && byte <= 0xfe) {
          const point = euckr[(lead - 0x81) * 190 + byte - 0x41];
          if (point !== 0) {
            units[count++] = point;
            continue;
          }
        }
        if (fatal) {
          return this.#fail(byte < 0x80 ? [byte] : []);
        }
        units[count++] = 0xfffd;
        if (byte < 0x80) {
          index--;
        }
        continue;
      }
      if (byte < 0x80) {
        units[count++] = byte;
      } else if (byte >= 0x81 && byte <= 0xfe) {
        leading = byte;
      } else if (fatal) {
        return this.#fail();
      } else {
        units[count++] = 0xfffd;
      }
    }
    if (flush && leading !== 0) {
      if (fatal) {
        return this.#fail();
      }
      units[count++] = 0xfffd;
    }
    this.#leading = leading;
    return stringOf(units, count);
  }

  #fail(restored: readonly number[] = []): undefined {
    this.#leading = 0;
    this.restored = restored;
    return undefined;
  }
}

class UTF16Decoder implements Decoder {
  readonly #bigEndian: boolean;
  #leadingByte = -1;
  #leadingSurrogate = 0;
  restored: readonly number[] = [];

  constructor(bigEndian: boolean) {
    this.#bigEndian = bigEndian;
  }

  decode(
    bytes: Uint8Array,
    flush: boolean,
    fatal: boolean,
  ): string | undefined {
    const bigEndian = this.#bigEndian;
    const units = new Uint16Array((bytes.length >> 1) + 8);
    let count = 0;
    let leadingByte = this.#leadingByte;
    let leadingSurrogate = this.#leadingSurrogate;
    for (let index = 0; index < bytes.length; index++) {
      if (leadingByte < 0) {
        leadingByte = bytes[index];
        continue;
      }
      const unit = bigEndian
        ? (leadingByte << 8) | bytes[index]
        : (bytes[index] << 8) | leadingByte;
      leadingByte = -1;
      if (leadingSurrogate !== 0) {
        const lead = leadingSurrogate;
        leadingSurrogate = 0;
        if (unit >= 0xdc00 && unit <= 0xdfff) {
          units[count++] = lead;
          units[count++] = unit;
          continue;
        }
        if (fatal) {
          return this.#fail(
            bigEndian ? [unit >> 8, unit & 0xff] : [unit & 0xff, unit >> 8],
          );
        }
        units[count++] = 0xfffd;
      }
      if (unit >= 0xd800 && unit <= 0xdbff) {
        leadingSurrogate = unit;
      } else if (unit >= 0xdc00 && unit <= 0xdfff) {
        if (fatal) {
          return this.#fail();
        }
        units[count++] = 0xfffd;
      } else {
        units[count++] = unit;
      }
    }
    if (flush && (leadingByte >= 0 || leadingSurrogate !== 0)) {
      if (fatal) {
        return this.#fail();
      }
      units[count++] = 0xfffd;
    }
    this.#leadingByte = leadingByte;
    this.#leadingSurrogate = leadingSurrogate;
    return stringOf(units, count);
  }

  #fail(restored: readonly number[] = []): undefined {
    this.#leadingByte = -1;
    this.#leadingSurrogate = 0;
    this.restored = restored;
    return undefined;
  }
}

const decoders: Partial<
  Record<Encoding, (tables: () => EncodingTables) => Decoder>
> = {
  gbk: (tables) => new GB18030Decoder(tables()),
  gb18030: (tables) => new GB18030Decoder(tables()),
  big5: (tables) => new Big5Decoder(tables()),
  "euc-jp": (tables) => new EUCJPDecoder(tables()),
  "iso-2022-jp": (tables) => new ISO2022JPDecoder(tables()),
  shift_jis: (tables) => new ShiftJISDecoder(tables()),
  "euc-kr": (tables) => new EUCKRDecoder(tables()),
  "utf-16be": () => new UTF16Decoder(true),
  "utf-16le": () => new UTF16Decoder(false),
  "x-user-defined": () => new SingleByteDecoder(userDefined),
};

const nativeSize = 1024;

const incomplete = (bytes: Uint8Array): number => {
  for (let back = 1; back <= 3 && back <= bytes.length; back++) {
    const lead = bytes[bytes.length - back];
    if (lead < 0x80) {
      return bytes.length;
    }
    if (lead < 0xc0) {
      continue;
    }
    const needed =
      lead >= 0xc2 && lead <= 0xdf
        ? 1
        : lead >= 0xe0 && lead <= 0xef
          ? 2
          : lead >= 0xf0 && lead <= 0xf4
            ? 3
            : 0;
    if (needed < back) {
      return bytes.length;
    }
    if (back > 1) {
      const next = bytes[bytes.length - back + 1];
      const lower = lead === 0xe0 ? 0xa0 : lead === 0xf0 ? 0x90 : 0x80;
      const upper = lead === 0xed ? 0x9f : lead === 0xf4 ? 0x8f : 0xbf;
      if (next < lower || next > upper) {
        return bytes.length;
      }
    }
    return bytes.length - back;
  }
  return bytes.length;
};

const bom = [0xef, 0xbb, 0xbf];

const textDecoderOptions = dictionary<{ fatal: boolean; ignoreBOM: boolean }>({
  fatal: { convert: boolean, fallback: false },
  ignoreBOM: { convert: boolean, fallback: false },
});
const textDecodeOptions = dictionary<{ stream: boolean }>({
  stream: { convert: boolean, fallback: false },
});

export const textDecoderClass = (
  tables: () => EncodingTables,
  nativeDecode?: NativeDecode,
) =>
  defineInterface(
    class TextDecoder {
      readonly #encoding: Encoding;
      readonly #fatal: boolean;
      readonly #ignoreBOM: boolean;
      #decoder: Decoder | undefined;
      #pending = new Uint8Array(0);
      #started = false;

      static [brand](value: object) {
        return #encoding in value;
      }

      constructor(
        label = "utf-8",
        options?: { fatal?: boolean; ignoreBOM?: boolean },
      ) {
        const encoding = encodingOf(label);
        if (encoding === undefined || encoding === "replacement") {
          throw new RangeError(
            `the encoding ${JSON.stringify(label)} is not supported`,
          );
        }
        this.#encoding = encoding;
        this.#fatal = options!.fatal!;
        this.#ignoreBOM = options!.ignoreBOM!;
      }

      get encoding(): string {
        return this.#encoding;
      }

      get fatal(): boolean {
        return this.#fatal;
      }

      get ignoreBOM(): boolean {
        return this.#ignoreBOM;
      }

      decode(
        input?: AllowSharedBufferSource,
        options?: { stream?: boolean },
      ): string {
        const stream = options!.stream!;
        const incoming =
          input === undefined ? new Uint8Array(0) : allowSharedBytesOf(input);
        let bytes = incoming;
        if (this.#pending.length > 0) {
          bytes = new Uint8Array(this.#pending.length + incoming.length);
          bytes.set(this.#pending);
          bytes.set(incoming, this.#pending.length);
        }
        if (this.#encoding !== "utf-8") {
          return this.#decodeLegacy(bytes, stream);
        }
        let index = 0;
        const started = this.#started;
        if (!started && !this.#ignoreBOM) {
          const prefix = Math.min(bytes.length, 3);
          if (
            stream &&
            prefix < 3 &&
            bytes
              .subarray(0, prefix)
              .every((byte, position) => byte === bom[position])
          ) {
            this.#pending = bytes.slice();
            return "";
          }
          if (
            prefix === 3 &&
            bytes[0] === 0xef &&
            bytes[1] === 0xbb &&
            bytes[2] === 0xbf
          ) {
            index = 3;
          }
        }
        if (bytes.length > 0) {
          this.#started = true;
        }
        if (nativeDecode !== undefined && bytes.length - index >= nativeSize) {
          const stop = stream ? incomplete(bytes) : bytes.length;
          const text = nativeDecode(bytes.subarray(index, stop), this.#fatal);
          if (typeof text !== "string") {
            return this.#fail(
              bytes,
              text < 0 ? text : index + text,
              stream,
              started,
            );
          }
          this.#pending = stream ? bytes.slice(stop) : new Uint8Array(0);
          if (!stream) {
            this.#started = false;
          }
          return text;
        }
        const units = new Uint16Array(bytes.length);
        let count = 0;
        let needed = 0;
        let seen = 0;
        let point = 0;
        let lower = 0x80;
        let upper = 0xbf;
        let sequence = 0;
        while (index < bytes.length) {
          const byte = bytes[index];
          if (needed === 0) {
            sequence = index;
            index++;
            if (byte < 0x80) {
              units[count++] = byte;
            } else if (byte >= 0xc2 && byte <= 0xdf) {
              needed = 1;
              point = byte & 0x1f;
            } else if (byte >= 0xe0 && byte <= 0xef) {
              lower = byte === 0xe0 ? 0xa0 : 0x80;
              upper = byte === 0xed ? 0x9f : 0xbf;
              needed = 2;
              point = byte & 0x0f;
            } else if (byte >= 0xf0 && byte <= 0xf4) {
              lower = byte === 0xf0 ? 0x90 : 0x80;
              upper = byte === 0xf4 ? 0x8f : 0xbf;
              needed = 3;
              point = byte & 0x07;
            } else if (this.#fatal) {
              return this.#fail(bytes, -1, stream, started);
            } else {
              units[count++] = 0xfffd;
            }
            continue;
          }
          if (byte < lower || byte > upper) {
            needed = seen = point = 0;
            lower = 0x80;
            upper = 0xbf;
            if (this.#fatal) {
              return this.#fail(bytes, index, stream, started);
            }
            units[count++] = 0xfffd;
            continue;
          }
          index++;
          lower = 0x80;
          upper = 0xbf;
          point = (point << 6) | (byte & 0x3f);
          if (++seen < needed) {
            continue;
          }
          if (point >= 0x10000) {
            point -= 0x10000;
            units[count++] = 0xd800 + (point >> 10);
            units[count++] = 0xdc00 + (point & 0x3ff);
          } else {
            units[count++] = point;
          }
          needed = seen = point = 0;
        }
        this.#pending = new Uint8Array(0);
        if (needed !== 0) {
          if (stream) {
            this.#pending = bytes.slice(sequence);
          } else if (this.#fatal) {
            return this.#fail(bytes, -1, stream, started);
          } else {
            units[count++] = 0xfffd;
          }
        }
        if (!stream) {
          this.#started = false;
        }
        return stringOf(units, count);
      }

      #fail(
        bytes: Uint8Array,
        restored: number,
        stream: boolean,
        started: boolean,
      ): never {
        this.#pending =
          stream && restored >= 0
            ? bytes.slice(restored, restored + 1)
            : new Uint8Array(0);
        this.#started = stream && started;
        throw new TypeError("the data is not valid UTF-8");
      }

      #decodeLegacy(bytes: Uint8Array, stream: boolean): string {
        const encoding = this.#encoding;
        const decoder = (this.#decoder ??=
          decoders[encoding]?.(tables) ??
          new SingleByteDecoder(expand(tables().singleByte[encoding])));
        let text = decoder.decode(bytes, !stream, this.#fatal);
        if (this.#pending.length > 0) {
          this.#pending = new Uint8Array(0);
        }
        if (!stream) {
          this.#decoder = undefined;
        }
        if (text === undefined) {
          if (stream) {
            this.#pending = Uint8Array.from(decoder.restored);
          } else {
            this.#started = false;
          }
          throw new TypeError(`the data is not valid ${encoding}`);
        }
        if (
          !this.#started &&
          !this.#ignoreBOM &&
          text !== "" &&
          (encoding === "utf-16le" || encoding === "utf-16be")
        ) {
          this.#started = true;
          if (text.charCodeAt(0) === 0xfeff) {
            text = text.slice(1);
          }
        }
        if (!stream) {
          this.#started = false;
        }
        return text;
      }
    },
    {
      construct: [optional(domString, "utf-8"), optional(textDecoderOptions)],
      operations: {
        decode: [
          optional(allowSharedBufferSource),
          optional(textDecodeOptions),
        ],
      },
    },
  );
