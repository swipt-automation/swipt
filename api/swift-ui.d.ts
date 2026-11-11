/** @available apiLevel 3 */
import type { CommonViewModifierProps } from "@expo/ui/swift-ui";
import type { ModifierConfig } from "@expo/ui/swift-ui/modifiers";
import type { ReactElement } from "react";
import type { ColorValue } from "react-native";

export type ChartValue = number | string | Date;

export type ChartDimension =
  number | { readonly ratio: number } | { readonly inset: number };

export type ChartForegroundStyle =
  ColorValue | { readonly gradient: readonly ColorValue[] };

export type ChartInterpolation =
  | "linear"
  | "catmullRom"
  | "cardinal"
  | "monotone"
  | "stepStart"
  | "stepCenter"
  | "stepEnd";

export type ChartSymbol =
  | "circle"
  | "square"
  | "triangle"
  | "diamond"
  | "pentagon"
  | "plus"
  | "cross"
  | "asterisk";

export type ChartStacking = "standard" | "normalized" | "center" | "unstacked";

export interface ChartLineStyle {
  readonly width?: number;
  readonly dash?: readonly number[];
  readonly cap?: "butt" | "round" | "square";
}

export interface ChartDatum {
  readonly series?: string;
  readonly group?: string;
  readonly color?: ColorValue;
}

export interface ChartPoint extends ChartDatum {
  readonly x: ChartValue;
  readonly y: ChartValue;
}

export interface ChartVerticalSpan extends ChartDatum {
  readonly x: ChartValue;
  readonly yStart: ChartValue;
  readonly yEnd: ChartValue;
}

export interface ChartHorizontalSpan extends ChartDatum {
  readonly xStart: ChartValue;
  readonly xEnd: ChartValue;
  readonly y: ChartValue;
}

export interface ChartArea extends ChartDatum {
  readonly xStart: ChartValue;
  readonly xEnd: ChartValue;
  readonly yStart: ChartValue;
  readonly yEnd: ChartValue;
}

export interface ChartSector extends ChartDatum {
  readonly value: number;
}

export interface ChartMarkStyle {
  readonly foregroundStyle?: ChartForegroundStyle;
  readonly opacity?: number;
}

export interface LineMark extends ChartMarkStyle {
  readonly type: "line";
  readonly data: readonly ChartPoint[];
  readonly interpolation?: ChartInterpolation;
  readonly lineStyle?: ChartLineStyle;
  readonly symbol?: ChartSymbol;
  readonly symbolSize?: number;
}

export interface AreaMark extends ChartMarkStyle {
  readonly type: "area";
  readonly data: readonly (
    ChartPoint | ChartVerticalSpan | ChartHorizontalSpan
  )[];
  readonly interpolation?: ChartInterpolation;
  readonly stacking?: ChartStacking;
}

export interface BarMark extends ChartMarkStyle {
  readonly type: "bar";
  readonly data: readonly (
    ChartPoint | ChartVerticalSpan | ChartHorizontalSpan
  )[];
  readonly stacking?: ChartStacking;
  readonly width?: ChartDimension;
  readonly height?: ChartDimension;
  readonly cornerRadius?: number;
}

export interface PointMark extends ChartMarkStyle {
  readonly type: "point";
  readonly data: readonly ChartPoint[];
  readonly symbol?: ChartSymbol;
  readonly symbolSize?: number;
}

export interface RuleMark extends ChartMarkStyle {
  readonly type: "rule";
  readonly data: readonly (
    | (ChartDatum & { readonly x: ChartValue })
    | (ChartDatum & { readonly y: ChartValue })
    | ChartVerticalSpan
    | ChartHorizontalSpan
  )[];
  readonly lineStyle?: ChartLineStyle;
}

export interface RectangleMark extends ChartMarkStyle {
  readonly type: "rectangle";
  readonly data: readonly (
    ChartPoint | ChartVerticalSpan | ChartHorizontalSpan | ChartArea
  )[];
  readonly width?: ChartDimension;
  readonly height?: ChartDimension;
  readonly cornerRadius?: number;
}

export interface SectorMark extends ChartMarkStyle {
  readonly type: "sector";
  readonly data: readonly ChartSector[];
  readonly innerRadius?: ChartDimension;
  readonly outerRadius?: ChartDimension;
  readonly angularInset?: number;
  readonly cornerRadius?: number;
}

export type ChartMark =
  | LineMark
  | AreaMark
  | BarMark
  | PointMark
  | RuleMark
  | RectangleMark
  | SectorMark;

export type ChartFormat =
  | {
      readonly style: "number";
      readonly fractionDigits?: number;
      readonly notation?: "compact";
      readonly scale?: number;
    }
  | {
      readonly style: "percent";
      readonly fractionDigits?: number;
      readonly scale?: number;
    }
  | {
      readonly style: "bytes";
      readonly units?: "memory" | "file" | "binary" | "decimal";
    }
  | {
      readonly style: "date";
      readonly date?:
        "omitted" | "numeric" | "abbreviated" | "long" | "complete";
      readonly time?: "omitted" | "shortened" | "standard" | "complete";
    };

export interface ChartAxis {
  readonly preset?: "automatic" | "extended" | "aligned" | "inset";
  readonly position?: "automatic" | "leading" | "trailing" | "top" | "bottom";
  readonly count?: number;
  readonly values?: readonly (
    ChartValue | { readonly value: ChartValue; readonly label: string }
  )[];
  readonly gridLines?: boolean;
  readonly ticks?: boolean;
  readonly labels?: boolean | ChartFormat;
  readonly title?: string;
}

export type ChartScaleType =
  "linear" | "log" | "squareRoot" | "symmetricLog" | "date" | "category";

export type ChartScale =
  | {
      readonly domain:
        readonly [number, number] | readonly [Date, Date] | readonly string[];
      readonly type?: ChartScaleType;
    }
  | {
      readonly domain?: undefined;
      readonly includesZero?: boolean;
      readonly reversed?: boolean;
      readonly type?: ChartScaleType;
    };

export interface ChartLegend {
  readonly position?:
    | "automatic"
    | "overlay"
    | "top"
    | "bottom"
    | "leading"
    | "trailing"
    | "topLeading"
    | "topTrailing"
    | "bottomLeading"
    | "bottomTrailing";
  readonly spacing?: number;
}

export interface ChartProps extends CommonViewModifierProps {
  readonly marks: readonly ChartMark[];
  readonly xAxis?: "automatic" | "hidden" | ChartAxis;
  readonly yAxis?: "automatic" | "hidden" | ChartAxis;
  readonly xScale?: ChartScale;
  readonly yScale?: ChartScale;
  readonly seriesColors?: Readonly<Record<string, ColorValue>>;
  readonly legend?: "automatic" | "hidden" | ChartLegend;
  readonly scrollableAxes?: "horizontal" | "vertical" | "both";
  readonly xVisibleDomain?: number;
  readonly yVisibleDomain?: number;
  readonly initialScrollX?: ChartValue;
  readonly initialScrollY?: ChartValue;
  readonly onXSelectionChange?: (value: ChartValue | undefined) => void;
  readonly onYSelectionChange?: (value: ChartValue | undefined) => void;
  readonly animated?: boolean;
}

export declare function Chart(props: ChartProps): ReactElement;

export type DataGridCellImage =
  | { readonly systemName: string; readonly color?: ColorValue }
  | { readonly uri: string };

export interface DataGridCell {
  readonly text: string;
  readonly image?: DataGridCellImage;
  readonly color?: ColorValue;
}

export interface DataGridFont {
  readonly textStyle?:
    | "largeTitle"
    | "title"
    | "title2"
    | "title3"
    | "headline"
    | "subheadline"
    | "body"
    | "callout"
    | "footnote"
    | "caption"
    | "caption2";
  readonly size?: number;
  readonly weight?:
    | "ultraLight"
    | "thin"
    | "light"
    | "regular"
    | "medium"
    | "semibold"
    | "bold"
    | "heavy"
    | "black";
  readonly design?: "default" | "rounded" | "monospaced" | "serif";
}

export interface DataGridColumn<Item> {
  readonly header: string;
  readonly cell: (item: Item) => string | number | DataGridCell;
  /** Only the cells that have been displayed widen a column; this widens it ahead of them. */
  readonly prototype?: string | number | DataGridCell;
  readonly minWidth?: number;
  readonly maxWidth?: number;
  /** The square space every cell keeps for its image; without it the cells show no image. */
  readonly imageSize?: number;
  readonly alignment?: "leading" | "center" | "trailing";
  readonly font?: DataGridFont;
  readonly monospacedDigit?: boolean;
}

export interface DataGridSortIndicator {
  readonly column: number;
  readonly order?: "ascending" | "descending";
}

export interface DataGridProps<Item> extends CommonViewModifierProps {
  readonly data: readonly Item[];
  readonly columns: readonly DataGridColumn<Item>[];
  readonly keyExtractor: (item: Item) => string;
  /** The rows beyond the visible ones that `cell` is also called for. */
  readonly overscanCount?: number;
  readonly frozenColumns?: number;
  readonly rowHeight?: number;
  readonly headerHeight?: number;
  readonly headerFont?: DataGridFont;
  readonly headerColor?: ColorValue;
  readonly rowColors?: readonly ColorValue[];
  readonly sortIndicator?: DataGridSortIndicator;
  readonly onCellPress?: (key: string, column: number) => void;
  readonly onHeaderPress?: (column: number) => void;
  readonly onHeaderLongPress?: (column: number) => void;
}

export declare function DataGrid<Item>(
  props: DataGridProps<Item>,
): ReactElement;

export declare function horizontalSizeClass(
  sizeClass: "compact" | "regular",
): ModifierConfig;
