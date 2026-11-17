import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve, relative } from 'node:path';
import { offSystemClasses } from './palette-guard';

test('the guard catches old-palette and tiny-text classes', () => {
  for (const bad of [
    'bg-gray-800',
    'hover:bg-gray-700',
    'text-brand-400',
    'border-yellow-700',
    'bg-brand-600/20',
    'md:hover:text-red-400',
    'text-white',
    'bg-black/60',
    'text-[10px]',
    'dark:bg-gray-950',
  ]) {
    assert.deepEqual(offSystemClasses(`<div className="p-2 ${bad} flex">`), [bad], bad);
  }
});

test('the guard passes VesperX classes and leaves comments and look-alikes alone', () => {
  const ok = `
    // was bg-gray-800 before the restyle
    /* text-brand-400 */
    <div className="bg-vx-surface text-vx-fg-muted border-vx-hairline font-brand text-[11px] text-xs bg-vx-accent/10 gray-800 my-text-gray-500x">
  `;
  assert.deepEqual(offSystemClasses(ok), []);
});

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && name !== 'palette-guard.ts' ? [p] : [];
  });
}

test('no source file uses classes off the VesperX system', () => {
  const src = resolve(process.cwd(), 'src');
  const found: string[] = [];
  for (const f of sources(src)) {
    for (const c of offSystemClasses(readFileSync(f, 'utf8'))) found.push(`${relative(src, f)}: ${c}`);
  }
  assert.deepEqual(found, []);
});
