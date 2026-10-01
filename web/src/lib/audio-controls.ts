import { loadAudioPrefs, saveAudioPrefs } from './audio-prefs';
import type { AudioState } from './device-audio';

type PrefsStorage = Parameters<typeof loadAudioPrefs>[0];

/** What the viewer's audio controls show. */
export interface AudioView {
  state: AudioState;
  reason?: string;
  muted: boolean;
  volume: number;
}

/** The parts of DeviceAudioPlayer the controls drive. */
export interface AudioPlayerLike {
  setOnStateChange(cb: (state: AudioState, reason?: string) => void): void;
  connect(serial: string): void;
  disconnect(): void;
  setMuted(muted: boolean): Promise<boolean>;
  setVolume(volume: number): void;
}

/**
 * Ties a device audio player to the viewer's mute button and volume slider,
 * remembering both per browser. A remembered "unmuted" is retried on start;
 * if the browser blocks autoplay the button shows muted until clicked.
 */
export class AudioControls {
  private view: AudioView;

  constructor(
    private readonly player: AudioPlayerLike,
    private readonly onChange: (view: AudioView) => void,
    private readonly storage?: PrefsStorage,
  ) {
    const prefs = loadAudioPrefs(storage);
    // Shown muted until playback actually starts.
    this.view = { state: 'connecting', muted: true, volume: prefs.volume };
    player.setOnStateChange((state, reason) => this.update({ state, reason }));
  }

  start(serial: string) {
    const prefs = loadAudioPrefs(this.storage);
    this.player.setVolume(prefs.volume);
    this.player.connect(serial);
    this.onChange(this.view);
    if (!prefs.muted) void this.unmute(false);
  }

  stop() {
    this.player.disconnect();
  }

  /** Click handler for the mute button. */
  async toggleMute() {
    if (this.view.muted) {
      await this.unmute(true);
    } else {
      await this.player.setMuted(true);
      this.update({ muted: true });
      this.save();
    }
  }

  setVolume(volume: number) {
    this.player.setVolume(volume);
    this.update({ volume });
    this.save();
  }

  private async unmute(remember: boolean) {
    const playing = await this.player.setMuted(false);
    if (!playing) return; // blocked or unsupported; the state says which
    this.update({ muted: false });
    if (remember) this.save();
  }

  private update(patch: Partial<AudioView>) {
    this.view = { ...this.view, ...patch };
    this.onChange(this.view);
  }

  private save() {
    saveAudioPrefs({ muted: this.view.muted, volume: this.view.volume }, this.storage);
  }
}
