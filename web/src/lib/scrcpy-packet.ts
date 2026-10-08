// The 12-byte header the backend relays in front of every scrcpy video and
// audio packet (scrcpy 4.0+): flags and PTS as a big-endian u64, then the
// payload size as a u32.
//   bit 63     session packet (video size; the backend consumes these)
//   bit 62     codec config
//   bit 61     keyframe
//   bits 0-60  PTS in microseconds
// Mirrors PacketFlag* in internal/device/protocol.go.

export interface PacketHeader {
  config: boolean;
  key: boolean;
  pts: number;
}

/** Parses a packet's header; null if it is too short or a session packet. */
export function parsePacketHeader(data: ArrayBuffer): PacketHeader | null {
  if (data.byteLength < 12) return null;
  const view = new DataView(data);
  const high = view.getUint32(0);
  if (high >>> 31) return null;
  return {
    config: ((high >>> 30) & 1) === 1,
    key: ((high >>> 29) & 1) === 1,
    pts: (high & 0x1fffffff) * 2 ** 32 + view.getUint32(4),
  };
}
