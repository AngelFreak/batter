'use client';

import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Camera, Keyboard, Clipboard, MoreHorizontal, Sun, Volume2, VolumeX } from 'lucide-react';
import { Menu, cx, type MenuItem } from '@/components/ui';
import type { AudioView } from '@/lib/audio-controls';
import { useNarrow } from './use-narrow';

interface ToolButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: ReactNode;
  label: string;
}

/**
 * One toolbar button: icon plus a mono label on wide screens, icon only (with
 * aria-label and title) on narrow ones, where the toolbar is a bottom bar.
 * A disabled button keeps pointer events so its title can say why.
 */
const ToolButton = forwardRef<HTMLButtonElement, ToolButtonProps>(function ToolButton(
  { icon, label, className, title, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      aria-label={label}
      title={title ?? label}
      className={cx(
        'flex h-14 min-w-touch flex-1 items-center justify-center gap-2 border-r border-vx-hairline last:border-r-0',
        'font-mono text-[11px] font-medium uppercase tracking-wide text-vx-fg-muted transition-colors',
        'hover:bg-vx-elevated hover:text-vx-fg',
        'aria-pressed:bg-vx-accent/10 aria-pressed:text-vx-accent',
        'disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-vx-fg-muted',
        'sm:h-11 sm:flex-none sm:px-4 focus-visible:[outline-offset:-2px]',
        className,
      )}
      {...rest}
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </button>
  );
});

export interface ViewerToolbarProps {
  audio: AudioView;
  audioUsable: boolean;
  audioTitle: string;
  onToggleMute: () => void;
  onVolume: (v: number) => void;
  keyboardOpen: boolean;
  onKeyboard: () => void;
  clipboardOpen: boolean;
  onClipboard: () => void;
  capturing: boolean;
  onScreenshot: () => void;
  onWake: () => void;
  /** Control socket is connected, so control buttons work. */
  controlOk: boolean;
  /** Why control buttons are disabled; shown as their title. */
  controlReason?: string;
  moreItems: (MenuItem | 'separator')[];
}

/**
 * The viewer's toolbar. Wide screens: a hairline bar under the header. Below
 * `sm`: a fixed bottom bar with five primary icons and More.
 */
export function ViewerToolbar(p: ViewerToolbarProps) {
  const narrow = useNarrow();
  const muteLabel =
    p.audio.state === 'connecting' || p.audio.state === 'reconnecting'
      ? 'Audio…'
      : !p.audioUsable
        ? 'No audio'
        : p.audio.muted
          ? 'Unmute'
          : 'Mute';
  const soundOn = p.audioUsable && !p.audio.muted;
  const controlTitle = (label: string) => (p.controlOk ? label : (p.controlReason ?? label));

  return (
    <div
      role="toolbar"
      aria-label="Device controls"
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      className={cx(
        'fixed inset-x-0 bottom-0 z-30 grid grid-cols-6 border-t border-vx-control bg-vx-recessed',
        'sm:static sm:z-auto sm:flex sm:shrink-0 sm:border-b sm:border-t-0 sm:border-vx-hairline sm:bg-vx-surface',
        'sm:!pb-0',
      )}
    >
      <div className="contents sm:flex sm:items-stretch sm:border-r sm:border-vx-hairline">
        <ToolButton
          icon={soundOn ? <Volume2 size={18} /> : <VolumeX size={18} />}
          label={muteLabel}
          title={p.audioTitle}
          aria-pressed={soundOn}
          disabled={!p.audioUsable}
          onClick={p.onToggleMute}
          className="sm:border-r-0"
        />
        {p.audioUsable && (
          <label className="hidden items-center gap-2.5 pl-1 pr-3.5 sm:flex">
            <span className="sr-only">Volume</span>
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={p.audio.volume}
              onChange={(e) => p.onVolume(Number(e.target.value))}
              title={`Volume ${Math.round(p.audio.volume * 100)}%`}
              className="w-24 accent-vx-accent"
            />
            <output className="min-w-[32px] font-mono text-[11px] tracking-wide text-vx-fg-muted">
              {Math.round(p.audio.volume * 100)}%
            </output>
          </label>
        )}
      </div>
      <ToolButton
        icon={<Keyboard size={18} />}
        label="Keyboard"
        title={controlTitle('Type text to send to the phone')}
        aria-pressed={p.keyboardOpen}
        disabled={!p.controlOk}
        onClick={p.onKeyboard}
      />
      <ToolButton
        icon={<Clipboard size={18} />}
        label="Clipboard"
        title={controlTitle('Send text to or fetch text from the phone clipboard')}
        aria-pressed={p.clipboardOpen}
        disabled={!p.controlOk}
        onClick={p.onClipboard}
      />
      <ToolButton
        icon={<Camera size={18} />}
        label={p.capturing ? 'Capturing…' : 'Screenshot'}
        title="Save a screenshot of the phone"
        disabled={p.capturing}
        onClick={p.onScreenshot}
      />
      <ToolButton
        icon={<Sun size={18} />}
        label="Wake"
        title={controlTitle('Wake the phone screen')}
        disabled={!p.controlOk}
        onClick={p.onWake}
      />
      <Menu
        side={narrow ? 'top' : 'bottom'}
        align="end"
        items={p.moreItems}
        trigger={(t) => <ToolButton {...t} icon={<MoreHorizontal size={18} />} label="More" title="More actions" />}
      />
    </div>
  );
}
