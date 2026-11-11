import {
  createModifier,
  createViewModifierEventListener,
} from "@expo/ui/swift-ui/modifiers";
import { requireNativeView } from "expo";
import { useMemo, useRef, useState, type ComponentType } from "react";

type Value = number | string | Date;
type Kind = "number" | "string" | "date";
type Fields = Record<string, unknown>;
type Modifiers = Parameters<typeof createViewModifierEventListener>[0];

const NativeChart = requireNativeView(
  "SwiptSwiftUI",
  "SwipletChartView",
) as ComponentType<Fields>;
const NativeDataGrid = requireNativeView(
  "SwiptSwiftUI",
  "SwipletDataGridView",
) as ComponentType<Fields>;

export const horizontalSizeClass = (sizeClass: "compact" | "regular") =>
  createModifier("swiptHorizontalSizeClass", { sizeClass });

const kindOf = (value: unknown): Kind | undefined =>
  value instanceof Date
    ? "date"
    : typeof value === "string"
      ? "string"
      : typeof value === "number"
        ? "number"
        : undefined;

const plain = (value: unknown) =>
  value instanceof Date ? value.getTime() : value;

const xFields = ["x", "xStart", "xEnd"];
const yFields = ["y", "yStart", "yEnd"];

const kindIn = (data: readonly Fields[], fields: readonly string[]) => {
  for (const datum of data) {
    for (const field of fields) {
      const kind = kindOf(datum[field]);
      if (kind !== undefined) {
        return kind;
      }
    }
  }
  return undefined;
};

const dimension = (value: unknown) =>
  typeof value === "number" ? { fixed: value } : value;

interface Mark extends Fields {
  data: readonly Fields[];
  foregroundStyle?: unknown;
  lineStyle?: { width?: number; dash?: readonly number[]; cap?: string };
}

const nativeMark = ({
  data,
  foregroundStyle,
  lineStyle,
  width,
  height,
  innerRadius,
  outerRadius,
  ...mark
}: Mark) => {
  const xKind = kindIn(data, xFields);
  const yKind = kindIn(data, yFields);
  const gradient =
    typeof foregroundStyle === "object" &&
    foregroundStyle !== null &&
    "gradient" in foregroundStyle
      ? foregroundStyle.gradient
      : undefined;
  return {
    ...mark,
    data:
      xKind === "date" || yKind === "date"
        ? data.map((datum) => {
            const copy = { ...datum };
            for (const field of [...xFields, ...yFields]) {
              if (field in copy) {
                copy[field] = plain(copy[field]);
              }
            }
            return copy;
          })
        : data,
    xKind,
    yKind,
    foregroundStyle: gradient === undefined ? foregroundStyle : undefined,
    gradient,
    lineWidth: lineStyle?.width,
    lineDash: lineStyle?.dash,
    lineCap: lineStyle?.cap,
    width: dimension(width),
    height: dimension(height),
    innerRadius: dimension(innerRadius),
    outerRadius: dimension(outerRadius),
  };
};

interface Format extends Fields {
  notation?: string;
  units?: string;
}

interface Axis {
  position?: string;
  values?: readonly (Value | { value: Value; label?: string })[];
  gridLines?: boolean;
  ticks?: boolean;
  labels?: boolean | Format;
  title?: string;
}

const nativeAxis = (axis: "automatic" | "hidden" | Axis | undefined) => {
  if (axis === undefined || axis === "automatic") {
    return undefined;
  }
  if (axis === "hidden") {
    return { hidden: true };
  }
  const { values, labels, ...rest } = axis;
  const native: Fields = { ...rest, labelsHidden: labels === false };
  if (typeof labels === "object") {
    const { notation, units, ...format } = labels;
    native.format = {
      ...format,
      compact: notation === "compact",
      bytes: units,
    };
  }
  if (values !== undefined) {
    const entries = values.map((entry) =>
      typeof entry === "object" && !(entry instanceof Date)
        ? entry
        : { value: entry, label: undefined },
    );
    const kind = kindOf(entries[0]?.value);
    if (kind === "string") {
      native.strings = entries.map((entry) => entry.value);
    } else {
      native.kind = kind;
      native.numbers = entries.map((entry) => plain(entry.value));
    }
    if (entries.some((entry) => entry.label !== undefined)) {
      native.labels = entries.map(
        (entry) => entry.label ?? String(entry.value),
      );
    }
  }
  return native;
};

interface Scale extends Fields {
  domain?: readonly Value[];
}

const nativeScale = (scale: Scale | undefined) => {
  if (scale === undefined) {
    return undefined;
  }
  const { domain, ...rest } = scale;
  if (domain === undefined) {
    return rest;
  }
  const kind = kindOf(domain[0]);
  return kind === "string"
    ? { ...rest, strings: domain }
    : { ...rest, kind, numbers: domain.map(plain) };
};

let lastRevision = 0;

interface ChartProps extends Fields {
  marks: readonly Mark[];
  xAxis?: "automatic" | "hidden" | Axis;
  yAxis?: "automatic" | "hidden" | Axis;
  xScale?: Scale;
  yScale?: Scale;
  seriesColors?: Record<string, unknown>;
  legend?: "automatic" | "hidden" | Fields;
  initialScrollX?: Value;
  initialScrollY?: Value;
  onXSelectionChange?: (value: Value | undefined) => void;
  onYSelectionChange?: (value: Value | undefined) => void;
  modifiers?: Modifiers;
}

export function Chart({
  marks,
  xAxis,
  yAxis,
  xScale,
  yScale,
  seriesColors,
  legend,
  initialScrollX,
  initialScrollY,
  onXSelectionChange,
  onYSelectionChange,
  modifiers,
  ...props
}: ChartProps) {
  const native = useMemo(() => {
    const converted = marks.map(nativeMark);
    return {
      marks: converted,
      xKind: converted.find((mark) => mark.xKind !== undefined)?.xKind,
      yKind: converted.find((mark) => mark.yKind !== undefined)?.yKind,
      revision: ++lastRevision,
    };
  }, [marks]);
  const selection =
    (
      kind: Kind | undefined,
      change: ((value: Value | undefined) => void) | undefined,
    ) =>
    ({ nativeEvent }: { nativeEvent: { value?: number | string } }) =>
      change?.(
        kind === "date" && typeof nativeEvent.value === "number"
          ? new Date(nativeEvent.value)
          : nativeEvent.value,
      );
  return (
    <NativeChart
      {...props}
      {...native}
      modifiers={modifiers}
      {...(modifiers ? createViewModifierEventListener(modifiers) : undefined)}
      xAxis={nativeAxis(xAxis)}
      yAxis={nativeAxis(yAxis)}
      xScale={nativeScale(xScale)}
      yScale={nativeScale(yScale)}
      seriesNames={seriesColors && Object.keys(seriesColors)}
      seriesColors={seriesColors && Object.values(seriesColors)}
      legend={
        legend === undefined || legend === "automatic"
          ? undefined
          : legend === "hidden"
            ? { hidden: true }
            : legend
      }
      initialScrollX={plain(initialScrollX)}
      initialScrollY={plain(initialScrollY)}
      selectsX={onXSelectionChange !== undefined}
      selectsY={onYSelectionChange !== undefined}
      onXSelectionChange={selection(native.xKind, onXSelectionChange)}
      onYSelectionChange={selection(native.yKind, onYSelectionChange)}
    />
  );
}

interface GridCell {
  text: string;
  image?: { systemName?: string; uri?: string; color?: unknown };
  color?: unknown;
}

interface GridColumn<Item> extends Fields {
  cell: (item: Item) => string | number | GridCell;
  prototype?: string | number | GridCell;
}

interface DataGridProps<Item> extends Fields {
  data: readonly Item[];
  columns: readonly GridColumn<Item>[];
  keyExtractor: (item: Item) => string;
  overscanCount?: number;
  headerFont?: Fields;
  headerColor?: unknown;
  sortIndicator?: { column: number; order?: "ascending" | "descending" };
  onCellPress?: (key: string, column: number) => void;
  onHeaderPress?: (column: number) => void;
  onHeaderLongPress?: (column: number) => void;
  modifiers?: Modifiers;
}

const nativeGridCell = (cell: string | number | GridCell) => {
  if (typeof cell !== "object") {
    return String(cell);
  }
  const { image, ...rest } = cell;
  return image === undefined
    ? rest
    : {
        ...rest,
        systemImage: image.systemName,
        image: image.uri,
        imageColor: image.color,
      };
};

export function DataGrid<Item>({
  data,
  columns,
  keyExtractor,
  overscanCount = 10,
  headerFont,
  headerColor,
  sortIndicator,
  onCellPress,
  onHeaderPress,
  onHeaderLongPress,
  modifiers,
  ...props
}: DataGridProps<Item>) {
  const extractor = useRef(keyExtractor);
  extractor.current = keyExtractor;
  const [visible, setVisible] = useState({ start: 0, end: 0 });
  const start = Math.max(visible.start - overscanCount, 0);
  const end = Math.min(visible.end + overscanCount, data.length - 1);
  const window = useMemo(
    () => ({
      start,
      rows: data.slice(start, end + 1).map((item) => ({
        key: extractor.current(item),
        cells: columns.map((column) => nativeGridCell(column.cell(item))),
      })),
    }),
    [data, columns, start, end],
  );
  const column =
    (handler: ((column: number) => void) | undefined) =>
    ({ nativeEvent }: { nativeEvent: { column: number } }) =>
      handler?.(nativeEvent.column);
  return (
    <NativeDataGrid
      {...props}
      modifiers={modifiers}
      {...(modifiers ? createViewModifierEventListener(modifiers) : undefined)}
      rowCount={data.length}
      window={window}
      columns={columns.map(({ cell, prototype, ...rest }) => ({
        ...rest,
        prototype:
          prototype === undefined ? undefined : nativeGridCell(prototype),
      }))}
      header={{ font: headerFont, color: headerColor }}
      sortIndicator={
        sortIndicator && {
          column: sortIndicator.column,
          descending: sortIndicator.order === "descending",
        }
      }
      onWindowChange={({
        nativeEvent,
      }: {
        nativeEvent: { start: number; end: number };
      }) => setVisible({ start: nativeEvent.start, end: nativeEvent.end })}
      onCellPress={({
        nativeEvent,
      }: {
        nativeEvent: { key: string; column: number };
      }) => onCellPress?.(nativeEvent.key, nativeEvent.column)}
      onHeaderPress={column(onHeaderPress)}
      onHeaderLongPress={column(onHeaderLongPress)}
    />
  );
}
