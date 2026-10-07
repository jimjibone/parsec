// Lane packing for a project's swimlane. Each task occupies one lane (row);
// tasks in the same lane must not overlap in time.

export interface Slot {
  id: string;
  s: number; // first day, inclusive
  e: number; // last day, inclusive
  lane: number;
}

function overlaps(a: Slot, s: number, e: number): boolean {
  return a.s <= e && s <= a.e;
}

function isFree(slots: Slot[], lane: number, s: number, e: number, exclude: string): boolean {
  return !slots.some((o) => o.id !== exclude && o.lane === lane && overlaps(o, s, e));
}

function nearestFree(slots: Slot[], from: number, s: number, e: number, exclude: string): number {
  for (let d = 0; ; d++) {
    if (isFree(slots, from + d, s, e, exclude)) return from + d;
    if (d > 0 && from - d >= 0 && isFree(slots, from - d, s, e, exclude)) return from - d;
  }
}

/** Renumber used lanes to 0..n-1, keeping their order. */
function compact(slots: Slot[]): void {
  const used = [...new Set(slots.map((x) => x.lane))].sort((a, b) => a - b);
  const map = new Map(used.map((l, i) => [l, i]));
  for (const x of slots) x.lane = map.get(x.lane)!;
}

/**
 * Resolve stored lanes into a conflict-free layout. Stored data can overlap
 * after edits elsewhere (tracker, git merges); later tasks get bumped.
 */
export function resolve(input: Slot[]): Slot[] {
  const slots = input.map((x) => ({ ...x }));
  const order = [...slots].sort((a, b) => a.lane - b.lane || a.s - b.s || a.id.localeCompare(b.id));
  const placed: Slot[] = [];
  for (const x of order) {
    x.lane = nearestFreeBelow(placed, x.lane, x.s, x.e);
    placed.push(x);
  }
  compact(slots);
  return slots;
}

function nearestFreeBelow(slots: Slot[], from: number, s: number, e: number): number {
  let l = Math.max(0, from);
  while (!isFree(slots, l, s, e, '')) l++;
  return l;
}

/**
 * Place `moved` (already holding its new s/e) into the layout.
 * - target undefined: keep its lane if free, else nearest free lane.
 * - target set: move to that lane; if occupied, insert a new lane there
 *   (above the occupied lane when moving up, below it when moving down).
 * Returns a new slot list with compacted lanes.
 */
export function place(input: Slot[], moved: Slot, target?: number): Slot[] {
  const slots = input.filter((x) => x.id !== moved.id).map((x) => ({ ...x }));
  const m = { ...moved };
  if (target === undefined || target === moved.lane) {
    m.lane = nearestFree(slots, m.lane, m.s, m.e, m.id);
  } else {
    const t = Math.max(0, target);
    if (isFree(slots, t, m.s, m.e, m.id)) {
      m.lane = t;
    } else {
      const at = t < moved.lane ? t : t + 1;
      for (const x of slots) if (x.lane >= at) x.lane++;
      m.lane = at;
    }
  }
  slots.push(m);
  compact(slots);
  return slots;
}
