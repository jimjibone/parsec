// Working-day aware date arithmetic for tasks. All values are day numbers
// (see dates.ts). With weekendWork off, Saturday and Sunday are not working
// days: durations exclude them and start/end dates never land on them.

import { weekday } from './dates';

export interface Span {
  s: number;
  e: number;
}

export function isWeekend(day: number): boolean {
  return weekday(day) >= 5;
}

/** Move off a weekend: dir 1 to the next Monday, -1 to the previous Friday. */
export function snapWorkday(day: number, dir: 1 | -1 = 1): number {
  while (isWeekend(day)) day += dir;
  return day;
}

/** Number of working days in [s, e]. */
export function workDays(s: number, e: number, weekendWork: boolean): number {
  if (weekendWork) return e - s + 1;
  let n = 0;
  for (let d = s; d <= e; d++) if (!isWeekend(d)) n++;
  return n;
}

/** Last day of a span starting at `start` that holds `n` working days. */
export function addWorkDays(start: number, n: number): number {
  let d = start;
  let c = isWeekend(d) ? 0 : 1;
  while (c < n) {
    d++;
    if (!isWeekend(d)) c++;
  }
  return d;
}

const dirOf = (delta: number): 1 | -1 => (delta < 0 ? -1 : 1);

/** Move a span by `delta` calendar days, keeping its working-day length. */
export function shiftSpan(s: number, e: number, delta: number, weekendWork: boolean): Span {
  if (weekendWork) return { s: s + delta, e: e + delta };
  const n = Math.max(1, workDays(s, e, false));
  const ns = snapWorkday(s + delta, dirOf(delta));
  return { s: ns, e: addWorkDays(ns, n) };
}

/** Span with start moved by `delta` calendar days; end unchanged. */
export function resizeStart(s: number, e: number, delta: number, weekendWork: boolean): Span {
  let ns = s + delta;
  if (!weekendWork) ns = snapWorkday(ns, dirOf(delta));
  return { s: Math.min(ns, e), e };
}

/** Span with end moved by `delta` calendar days; start unchanged. */
export function resizeEnd(s: number, e: number, delta: number, weekendWork: boolean): Span {
  let ne = e + delta;
  if (!weekendWork) ne = snapWorkday(ne, dirOf(delta));
  return { s, e: Math.max(ne, s) };
}

/** Pull start and end off weekends (start forwards, end backwards). */
export function normalizeSpan(s: number, e: number, weekendWork: boolean): Span {
  if (weekendWork) return { s, e };
  const ns = snapWorkday(s, 1);
  const ne = snapWorkday(e, -1);
  return { s: ns, e: Math.max(ne, ns) };
}
