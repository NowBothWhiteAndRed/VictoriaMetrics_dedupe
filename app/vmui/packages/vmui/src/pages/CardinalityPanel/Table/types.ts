import { MouseEvent, ReactNode } from "react";

export type Order = "asc" | "desc";

export interface HeadCell {
  id: string;
  label: string | ReactNode;
  info?: string;
  sortable?: boolean;
  modifiers?: string[]; // modifiers for styling
}

export interface EnhancedHeaderTableProps {
  onRequestSort: (event: MouseEvent<HTMLTableCellElement>, property: keyof Data) => void;
  order: Order;
  orderBy: string;
  rowCount: number;
  headerCells: HeadCell[];
}

export interface TableProps {
  rows: Data[];
  headerCells: HeadCell[],
  defaultSortColumn: keyof Data,
  tableCells: (row: Data) => ReactNode,
  isPagingEnabled?: boolean,
}


export interface Data {
  name: string;
  value: number;
  diff: number;
  diffPercent: number;
  valuePrev: number;
  progressValue: number;
  actions: string;
  lastRequestTimestamp: number;
  requestsCount: number;
}
