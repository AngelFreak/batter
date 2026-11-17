'use client';

import type { ReactNode } from 'react';
import { cx } from '@/components/ui';

export interface Column<T> {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  /** The row's title. On a phone it heads the card, without a label. */
  primary?: boolean;
  /** Extra classes for the cell. */
  className?: string;
}

export interface DataTableProps<T> {
  /** Read by screen readers; not drawn. */
  caption: string;
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  /** Buttons for the row's last cell. Always visible, never on hover. */
  actions?: (row: T) => ReactNode;
  /** True while a refresh is in flight. The rows stay on screen. */
  refreshing?: boolean;
}

/**
 * A hairline table. From the `sm` breakpoint up it is a table with mono column
 * headers. Below it, every row is a stacked card of label and value pairs, so
 * nothing scrolls sideways at phone width. Explicit ARIA roles keep the table
 * semantics when the elements are laid out as blocks.
 */
export function DataTable<T>({ caption, columns, rows, rowKey, actions, refreshing }: DataTableProps<T>) {
  return (
    <div className="sm:border sm:border-vx-hairline">
      <table role="table" aria-busy={refreshing || undefined} className="block w-full border-collapse sm:table">
        <caption className="sr-only">{caption}</caption>
        <thead role="rowgroup" className="hidden bg-vx-recessed sm:table-header-group">
          <tr role="row">
            {columns.map((c) => (
              <th
                key={c.key}
                role="columnheader"
                scope="col"
                className="vx-label border-b border-vx-hairline px-4 py-2.5 text-left font-normal"
              >
                {c.header}
              </th>
            ))}
            {actions && (
              <th role="columnheader" scope="col" className="border-b border-vx-hairline px-4 py-2.5">
                <span className="sr-only">Actions</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody role="rowgroup" className="block sm:table-row-group sm:[&>tr:last-child>td]:border-b-0">
          {rows.map((row) => (
            <tr
              key={rowKey(row)}
              role="row"
              className="mb-3 block border border-vx-hairline bg-vx-surface last:mb-0 sm:mb-0 sm:table-row sm:border-0"
            >
              {columns.map((c) => (
                <td
                  key={c.key}
                  role="cell"
                  className={cx(
                    'px-4 py-2.5 sm:table-cell sm:border-b sm:border-vx-hairline sm:py-3 sm:align-middle',
                    c.primary ? 'block pb-1 pt-3' : 'flex items-start justify-between gap-4',
                    c.className,
                  )}
                >
                  {!c.primary && <span className="vx-label shrink-0 pt-0.5 sm:hidden">{c.header}</span>}
                  <div className={cx('min-w-0 break-words', !c.primary && 'text-right sm:text-left')}>{c.cell(row)}</div>
                </td>
              ))}
              {actions && (
                <td
                  role="cell"
                  className="flex flex-wrap items-center justify-end gap-2 border-t border-vx-hairline px-4 py-2.5 sm:table-cell sm:whitespace-nowrap sm:border-b sm:border-t-0 sm:py-3 sm:text-right sm:align-middle"
                >
                  <div className="inline-flex items-center justify-end gap-2">{actions(row)}</div>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
