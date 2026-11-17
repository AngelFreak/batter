import type { Metadata, Viewport } from 'next';
import localFont from 'next/font/local';
import { AppShell } from '@/components/layout/app-shell';
import './globals.css';

// Self-hosted (OFL 1.1, see fonts/README.md): no font CDN.
const sans = localFont({
  src: './fonts/archivo.woff2',
  weight: '100 900',
  display: 'swap',
  variable: '--vx-font-sans',
});
const mono = localFont({
  src: './fonts/jetbrains-mono.woff2',
  weight: '400 800',
  display: 'swap',
  variable: '--vx-font-mono',
});

export const metadata: Metadata = {
  title: 'Batter',
  description: 'Remote Android phone management',
};

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  themeColor: '#0A0E12',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`${sans.variable} ${mono.variable}`}>
      <body className="min-h-dvh">
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}
