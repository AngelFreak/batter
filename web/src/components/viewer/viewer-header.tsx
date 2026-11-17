'use client';

import type { ReactNode } from 'react';
import Link from 'next/link';
import { ArrowLeft, ArrowRight } from 'lucide-react';
import { StatusChip } from '@/components/ui';
import { streamStatus } from '@/lib/device-status';
import { videoView, type ViewerLive } from './viewer-status';

interface ViewerHeaderProps {
  name: string;
  serial: string;
  /** Present only while the viewer is mounted. */
  live?: ViewerLive;
  /** Quality, Stop session. */
  children?: ReactNode;
}

/** The viewer's title row: where you are, how the stream is doing, and the session actions. */
export function ViewerHeader({ name, serial, live, children }: ViewerHeaderProps) {
  const denied = live?.control === 'denied';
  return (
    <header className="shrink-0 border-b border-vx-hairline bg-vx-surface px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
        <div className="min-w-0">
          <Link
            href="/dashboard"
            className="inline-flex min-h-[24px] items-center gap-1 font-mono text-[11px] uppercase tracking-label text-vx-fg-muted hover:text-vx-accent"
          >
            <ArrowLeft size={14} aria-hidden />
            Devices
          </Link>
          <h1 className="mt-1 truncate text-xl leading-tight">{name}</h1>
          <p className="font-mono text-[11px] uppercase tracking-wide text-vx-fg-label">{serial}</p>
        </div>
        {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
      </div>

      {live && (
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
          <span className="inline-flex items-center gap-2">
            <span className="vx-label">Video</span>
            <StatusChip status={videoView(live.video)} />
          </span>
          {denied ? (
            <StatusChip status={streamStatus('denied')} />
          ) : (
            <span className="inline-flex items-center gap-2">
              <span className="vx-label">Control</span>
              <StatusChip status={streamStatus(live.control)} />
            </span>
          )}
          {live.keysToPhone && (
            <span className="inline-flex items-center gap-1.5 rounded-sm border border-vx-accent/40 bg-vx-accent/10 px-2 py-1 font-mono text-[11px] uppercase leading-none tracking-wide text-vx-accent">
              Keys
              <ArrowRight size={12} aria-hidden />
              <span className="sr-only">go to the</span>
              Phone
            </span>
          )}
          <p className="font-mono text-[11px] uppercase tracking-wide text-vx-fg-label">
            Details: video {live.video} · control {live.control}
            {live.fps > 0 ? ` · ${live.fps} fps` : ''}
          </p>
        </div>
      )}
    </header>
  );
}
