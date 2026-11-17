'use client';

import { useRef, type PointerEvent as ReactPointerEvent, type ReactNode, type RefObject } from 'react';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Move } from 'lucide-react';
import type { SwipeDirection } from '@/lib/swipe-pad';
import { clampPadPos, type PadPos } from '@/lib/viewer-prefs';

const ARROWS: { dir: SwipeDirection; label: string; cell: string; icon: ReactNode }[] = [
  { dir: 'up', label: 'Swipe up', cell: 'col-start-2 row-start-1', icon: <ArrowUp size={18} /> },
  { dir: 'left', label: 'Swipe left', cell: 'col-start-1 row-start-2', icon: <ArrowLeft size={18} /> },
  { dir: 'right', label: 'Swipe right', cell: 'col-start-3 row-start-2', icon: <ArrowRight size={18} /> },
  { dir: 'down', label: 'Swipe down', cell: 'col-start-2 row-start-3', icon: <ArrowDown size={18} /> },
];

interface SwipePadProps {
  /** The view the pad floats over; its position is a fraction of this. */
  boundsRef: RefObject<HTMLElement | null>;
  pos: PadPos;
  onMove: (pos: PadPos) => void;
  /** Called once a drag ends, with where the pad was left. */
  onMoved: (pos: PadPos) => void;
  onSwipe: (dir: SwipeDirection) => void;
  disabled?: boolean;
  /** Why the arrows are disabled; shown as their title. */
  disabledReason?: string;
}

/**
 * A floating pad of swipe arrows over the phone view. Drag it by its
 * middle; each arrow swipes a finger across the phone's screen that way.
 */
export function SwipePad({ boundsRef, pos, onMove, onMoved, onSwipe, disabled, disabledReason }: SwipePadProps) {
  const padRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ dx: number; dy: number; last: PadPos } | null>(null);

  // The pad's centre for a pointer position, kept fully inside the view.
  const placeAt = (clientX: number, clientY: number): PadPos | null => {
    const bounds = boundsRef.current?.getBoundingClientRect();
    const pad = padRef.current?.getBoundingClientRect();
    if (!bounds || !pad || !drag.current || bounds.width === 0 || bounds.height === 0) return null;
    const halfW = pad.width / 2 / bounds.width;
    const halfH = pad.height / 2 / bounds.height;
    const x = (clientX - drag.current.dx - bounds.left) / bounds.width;
    const y = (clientY - drag.current.dy - bounds.top) / bounds.height;
    const p = clampPadPos({ x, y });
    return {
      x: Math.min(Math.max(p.x, halfW), 1 - halfW),
      y: Math.min(Math.max(p.y, halfH), 1 - halfH),
    };
  };

  const startDrag = (e: ReactPointerEvent<HTMLDivElement>) => {
    const pad = padRef.current?.getBoundingClientRect();
    if (!pad) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    // Keep the grab point under the pointer rather than jumping the centre to it.
    drag.current = { dx: e.clientX - (pad.left + pad.width / 2), dy: e.clientY - (pad.top + pad.height / 2), last: pos };
  };

  const moveDrag = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    const p = placeAt(e.clientX, e.clientY);
    if (p) {
      drag.current.last = p;
      onMove(p);
    }
  };

  const endDrag = () => {
    if (!drag.current) return;
    onMoved(drag.current.last);
    drag.current = null;
  };

  return (
    <div
      ref={padRef}
      role="group"
      aria-label="Swipe pad"
      className="absolute z-20 grid select-none grid-cols-3 grid-rows-3 gap-px overflow-hidden rounded-md border border-vx-control bg-vx-hairline"
      style={{ left: `${pos.x * 100}%`, top: `${pos.y * 100}%`, transform: 'translate(-50%, -50%)' }}
    >
      {[0, 1, 2, 3].map((i) => (
        <span
          key={i}
          aria-hidden
          className={`bg-vx-surface ${['col-start-1 row-start-1', 'col-start-3 row-start-1', 'col-start-1 row-start-3', 'col-start-3 row-start-3'][i]}`}
        />
      ))}
      {ARROWS.map(({ dir, label, cell, icon }) => (
        <button
          key={dir}
          type="button"
          onClick={() => onSwipe(dir)}
          // Keep keyboard focus on the device screen so typing still goes to it.
          onMouseDown={(e) => e.preventDefault()}
          disabled={disabled}
          title={disabled && disabledReason ? disabledReason : label}
          aria-label={label}
          className={`${cell} flex h-11 w-11 items-center justify-center bg-vx-surface text-vx-fg-muted transition-colors hover:bg-vx-elevated hover:text-vx-fg disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-vx-surface disabled:hover:text-vx-fg-muted focus-visible:[outline-offset:-2px]`}
        >
          {icon}
        </button>
      ))}
      <div
        onPointerDown={startDrag}
        onPointerMove={moveDrag}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onMouseDown={(e) => e.preventDefault()}
        title="Drag to move"
        aria-label="Move the swipe pad"
        className="col-start-2 row-start-2 flex h-11 w-11 cursor-grab touch-none items-center justify-center bg-vx-surface text-vx-fg-label hover:text-vx-fg active:cursor-grabbing"
      >
        <Move size={16} />
      </div>
    </div>
  );
}
