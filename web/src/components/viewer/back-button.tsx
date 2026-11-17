import Link from 'next/link';
import { ArrowLeft } from 'lucide-react';

/** A link back to the device list, styled like a secondary button. */
export function BackButton() {
  return (
    <Link
      href="/dashboard"
      className="inline-flex h-touch items-center justify-center gap-2 rounded-sm border border-vx-control px-5 font-mono text-[12.5px] font-medium uppercase tracking-wide text-vx-fg transition-colors hover:border-vx-accent hover:text-vx-accent"
    >
      <ArrowLeft size={16} aria-hidden />
      Back
    </Link>
  );
}
