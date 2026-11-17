import { getToken } from "./auth";

const WS_BASE_URL = process.env.NEXT_PUBLIC_WS_URL || "";

/**
 * Whether the control socket for `serial` is refused for lack of permission.
 *
 * The server checks permission before the WebSocket upgrade and answers 403,
 * but a browser WebSocket only sees a failed connection. The same URL fetched
 * as plain HTTP gets the same check and shows the status. If permission is
 * fine the handler answers 404 or 400 before claiming control, so asking
 * changes nothing on the phone.
 */
export async function controlDenied(serial: string): Promise<boolean> {
  const base = WS_BASE_URL
    ? WS_BASE_URL.replace(/^ws/, "http")
    : `${window.location.protocol}//${window.location.host}`;
  const url = `${base}/ws/device/${encodeURIComponent(serial)}/control?token=${encodeURIComponent(getToken() || "")}`;
  try {
    const res = await fetch(url);
    return res.status === 403;
  } catch {
    return false;
  }
}
