export interface DataTarget {
  id: string;
  wholeDirectory: (name: string) => string;
  subdirectory: (name: string) => string;
  args: string[];
}

export type ResolveTarget = (args: string[]) => DataTarget;

export const operandCount = (operand: string) => (operand === "" ? 0 : 1);
