import type { Person } from './types';

// Mid-tone colours that carry white text and stay distinct from each other
// in both themes.
export const PERSON_COLORS = [
  '#d9534f',
  '#e67e22',
  '#c9a227',
  '#4caf50',
  '#16a085',
  '#2e86de',
  '#6c5ce7',
  '#b03a9a',
  '#e84393',
  '#795548',
  '#607d8b',
  '#00838f',
];

export const UNKNOWN_PERSON_COLOR = '#808080';

function leastUsed(used: Map<string, number>): string {
  const min = Math.min(...used.values());
  return PERSON_COLORS.find((c) => used.get(c) === min)!;
}

function bump(used: Map<string, number>, c: string) {
  if (used.has(c)) used.set(c, used.get(c)! + 1);
}

/**
 * Colour per person id. Saved colours are kept; people without one get the
 * least-used palette colour in list order, so defaults stay distinct
 * without writing anything back to the data repo.
 */
export function assignColors(people: Person[]): Map<string, string> {
  const used = new Map(PERSON_COLORS.map((c) => [c, 0]));
  const out = new Map<string, string>();
  for (const p of people) {
    if (!p.color) continue;
    out.set(p.id, p.color);
    bump(used, p.color);
  }
  for (const p of people) {
    if (p.color) continue;
    const c = leastUsed(used);
    out.set(p.id, c);
    bump(used, c);
  }
  return out;
}

/** Least-used palette colour, for a newly created person. */
export function nextPersonColor(people: Person[]): string {
  const used = new Map(PERSON_COLORS.map((c) => [c, 0]));
  for (const c of assignColors(people).values()) bump(used, c);
  return leastUsed(used);
}

export function initials(name: string | undefined): string {
  return (name ?? '?')
    .split(/\s+/)
    .filter(Boolean)
    .map((w) => w[0])
    .join('')
    .slice(0, 2)
    .toUpperCase();
}
