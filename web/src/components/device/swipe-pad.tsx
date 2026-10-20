'use client';

import { useRef, type PointerEvent as ReactPointerEvent, type RefObject } from 'react';
import type { SwipeDirection } from '@/lib/swipe-pad';
import { clampPadPos, type PadPos } from '@/lib/viewer-prefs';

const ARROWS: { dir: SwipeDirection; label: string; cell: string; points: string }[] = [
  { dir: 'up', label: 'Swipe up', cell: 'col-start-2 row-start-1', points: '6,15 12,9 18,15' },
  { dir: 'left', label: 'Swipe left', cell: 'col-start-1 row-start-2', points: '15,6 9,12 15,18' },
  { dir: 'right', label: 'Swipe right', cell: 'col-start-3 row-start-2', points: '9,6 15,12 9,18' },
  { dir: 'down', label: 'Swipe down', cell: 'col-start-2 row-start-3', points: '6,9 12,15 18,9' },
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
}

/**
 * A floating pad of swipe arrows over the phone view. Drag it by its
 * middle; each arrow swipes a finger across the phone's screen that way.
 */
export function SwipePad({ boundsRef, pos, onMove, onMoved, onSwipe, disabled }: SwipePadProps) {
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
      className="absolute z-20 grid grid-cols-3 grid-rows-3 gap-0.5 p-1 rounded-xl bg-gray-900/80 border border-gray-700 shadow-lg backdrop-blur-sm select-none"
      style={{ left: `${pos.x * 100}%`, top: `${pos.y * 100}%`, transform: 'translate(-50%, -50%)' }}
    >
      {ARROWS.map(({ dir, label, cell, points }) => (
        <button
          key={dir}
          type="button"
          onClick={() => onSwipe(dir)}
          // Keep keyboard focus on the device screen so typing still goes to it.
          onMouseDown={(e) => e.preventDefault()}
          disabled={disabled}
          title={label}
          aria-label={label}
          className={`${cell} flex items-center justify-center w-11 h-11 rounded-lg text-gray-300 hover:text-white hover:bg-gray-700 disabled:opacity-40 disabled:hover:bg-transparent`}
        >
          <svg viewBox="0 0 24 24" className="w-5 h-5" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <polyline points={points} />
          </svg>
        </button>
      ))}
      <div
        onPointerDown={startDrag}
        onPointerMove={moveDrag}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        title="Drag to move"
        aria-label="Move the swipe pad"
        className="col-start-2 row-start-2 flex items-center justify-center w-11 h-11 rounded-lg text-gray-500 hover:text-gray-300 cursor-grab active:cursor-grabbing touch-none"
      >
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
          {[8, 12, 16].flatMap((y) => [9, 15].map((x) => <circle key={`${x}-${y}`} cx={x} cy={y} r="1.5" />))}
        </svg>
      </div>
    </div>
  );
}
