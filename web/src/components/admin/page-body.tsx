import type { ReactNode } from 'react';

/** The padded column under a PageHeader. */
export function PageBody({ children }: { children: ReactNode }) {
  return <div className="mx-auto w-full max-w-5xl space-y-4 px-4 py-6 sm:px-6">{children}</div>;
}
