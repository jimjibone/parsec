import { describe, expect, it } from 'vitest';
import { placeMany, type Slot } from './lanes';

const lanes = (slots: Slot[]) => Object.fromEntries(slots.map((x) => [x.id, x.lane]));
const span = (slots: Slot[], id: string) => {
  const x = slots.find((y) => y.id === id)!;
  return [x.s, x.e];
};

describe('placeMany', () => {
  it('keeps lanes when the moved group has room', () => {
    const input = [
      { id: 'a', s: 0, e: 2, lane: 0 },
      { id: 'b', s: 0, e: 2, lane: 1 },
      { id: 'c', s: 10, e: 12, lane: 0 },
    ];
    const out = placeMany(input, [
      { id: 'a', s: 3, e: 5, lane: 0 },
      { id: 'b', s: 3, e: 5, lane: 0 },
    ]);
    expect(lanes(out)).toEqual({ a: 0, b: 1, c: 0 });
    expect(span(out, 'a')).toEqual([3, 5]);
    expect(span(out, 'b')).toEqual([3, 5]);
    expect(span(out, 'c')).toEqual([10, 12]);
  });

  it('moves two tasks in one lane together without them bumping each other', () => {
    const input = [
      { id: 'a', s: 0, e: 2, lane: 0 },
      { id: 'b', s: 3, e: 5, lane: 0 },
    ];
    const out = placeMany(input, [
      { id: 'a', s: 3, e: 5, lane: 0 },
      { id: 'b', s: 6, e: 8, lane: 0 },
    ]);
    expect(lanes(out)).toEqual({ a: 0, b: 0 });
  });

  it('bumps a moved task that lands on an unmoved one', () => {
    const input = [
      { id: 'a', s: 0, e: 2, lane: 0 },
      { id: 'b', s: 6, e: 8, lane: 0 },
    ];
    const out = placeMany(input, [{ id: 'a', s: 5, e: 7, lane: 0 }]);
    expect(lanes(out)).toEqual({ a: 1, b: 0 });
  });

  it('leaves no overlaps in any lane', () => {
    const input = [
      { id: 'a', s: 0, e: 4, lane: 0 },
      { id: 'b', s: 0, e: 4, lane: 1 },
      { id: 'c', s: 5, e: 9, lane: 0 },
      { id: 'd', s: 5, e: 9, lane: 1 },
    ];
    const out = placeMany(input, [
      { id: 'a', s: 3, e: 7, lane: 0 },
      { id: 'b', s: 3, e: 7, lane: 0 },
    ]);
    for (const x of out) {
      for (const y of out) {
        if (x.id < y.id && x.lane === y.lane) expect(x.e < y.s || y.e < x.s).toBe(true);
      }
    }
  });
});
