import { useEffect } from 'react';

/**
 * While mounted, stops the browser's two-finger back/forward swipe, so a
 * horizontal trackpad swipe goes to the phone instead of leaving the page.
 */
export function useNoSwipeNavigation() {
  useEffect(() => {
    const html = document.documentElement;
    const prev = html.style.overscrollBehaviorX;
    html.style.overscrollBehaviorX = 'none';
    return () => {
      html.style.overscrollBehaviorX = prev;
    };
  }, []);
}
