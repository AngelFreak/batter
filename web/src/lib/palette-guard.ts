// Finds styling that is off the VesperX system: Tailwind colour utilities on
// the old stock palettes, and text smaller than 11px. Used by
// palette-guard.test.ts to scan the source.

const PREFIXES =
  'bg|text|border|border-[trblxy]|ring|ring-offset|divide|placeholder|outline|fill|stroke|from|via|to|accent|caret|decoration|shadow';
const OLD_PALETTES = 'gray|brand|indigo|slate|zinc|neutral|stone|red|green|yellow|blue|purple|emerald|amber|orange|sky|cyan|teal|rose|pink|violet|fuchsia|lime';

const COLOUR = new RegExp(`(?<![\\w-])(?:[\\w-]+:)*(?:${PREFIXES})-(?:${OLD_PALETTES})-\\d{2,3}(?:\\/\\d{1,3})?(?![\\w-])`, 'g');
const COLOUR_KEYWORD = new RegExp(`(?<![\\w-])(?:[\\w-]+:)*(?:bg|text|border)-(?:white|black)(?:\\/\\d{1,3})?(?![\\w-])`, 'g');
const TINY_TEXT = /(?<![\w-])(?:[\w-]+:)*text-\[(?:[0-9]|10)(?:\.\d+)?px\](?![\w-])/g;
const DARK_VARIANT = /(?<![\w-])dark:[\w[\]/.-]+/g;

/** Strip // and /* *\/ comments so prose about old classes isn't flagged. */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:"'`])\/\/.*$/gm, '$1');
}

export function offSystemClasses(source: string): string[] {
  const code = stripComments(source);
  const found = [
    ...(code.match(COLOUR) ?? []),
    ...(code.match(COLOUR_KEYWORD) ?? []),
    ...(code.match(TINY_TEXT) ?? []),
    ...(code.match(DARK_VARIANT) ?? []),
  ];
  // A class can match two rules (dark:bg-gray-950); report it once.
  return Array.from(new Set(found));
}
