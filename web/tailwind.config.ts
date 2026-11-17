import type { Config } from 'tailwindcss';

// VesperX screen-mode tokens. The values live as RGB channels in globals.css,
// so opacity modifiers work: bg-vx-accent/10.
const vx = (name: string) => `rgb(var(--vx-${name}) / <alpha-value>)`;

const config: Config = {
  content: ['./src/**/*.{js,ts,jsx,tsx,mdx}'],
  theme: {
    extend: {
      // Below `nav` the sidebar becomes a drawer.
      screens: { nav: '820px' },
      colors: {
        vx: {
          canvas: vx('canvas'),
          recessed: vx('recessed'),
          surface: vx('surface'),
          elevated: vx('elevated'),
          hairline: vx('hairline'),
          control: vx('control'),
          fg: vx('fg'),
          'fg-muted': vx('fg-muted'),
          'fg-label': vx('fg-label'),
          'fg-dim': vx('fg-dim'),
          accent: vx('accent'),
          'accent-hover': vx('accent-hover'),
          'accent-ink': vx('accent-ink'),
          warn: vx('warn'),
          danger: vx('danger'),
        },
      },
      fontFamily: {
        sans: ['var(--vx-font-sans)', 'system-ui', 'Segoe UI', 'Helvetica', 'Arial', 'sans-serif'],
        mono: ['var(--vx-font-mono)', 'ui-monospace', 'SFMono-Regular', 'Consolas', 'monospace'],
      },
      // The brand is square: 0 for cards, 2px for controls, 4px for panels.
      borderRadius: {
        none: '0',
        sm: '2px',
        DEFAULT: '2px',
        md: '4px',
        lg: '4px',
        xl: '4px',
        '2xl': '4px',
      },
      letterSpacing: {
        label: '0.12em',
        wide: '0.08em',
        eyebrow: '0.18em',
        headline: '-0.02em',
      },
      height: { touch: '44px' },
      width: { touch: '44px' },
      minHeight: { touch: '44px' },
      minWidth: { touch: '44px' },
      transitionDuration: { DEFAULT: '150ms' },
    },
  },
  plugins: [],
};

export default config;
