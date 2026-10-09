// Task pressure: a task's estimate against the working time its bar allows,
// given who is assigned. Pure functions only, so callers can pass live drag
// spans and tests need no Svelte.

import { workDays } from './schedule';

export type PressureLevel = 'ok' | 'tight' | 'over';
export const TIGHT_AT = 0.8;
export const OVER_AT = 1.0;

export interface Pressure {
  ratio: number; // estimate / capacity
  level: PressureLevel;
  estimate: number; // hours
  capacity: number; // hours available in the bar
  dailyHours: number; // effective team hours per working day
  days: number; // working days in the bar
  neededDays: number; // ceil(estimate / dailyHours): working days the work needs
  assumed: boolean; // no assignees; one person at the team default assumed
}

export interface PlanningLike {
  hoursPerDay: number;
  assigneeFactor: number;
}

/** Effective team hours per working day: the person with the most hours counts in full, each other person counts x factor. */
export function dailyHours(hours: number[], factor: number): number {
  if (!hours.length) return 0;
  const h = [...hours].sort((a, b) => b - a);
  return h[0] + factor * h.slice(1).reduce((sum, x) => sum + x, 0);
}

export function levelOf(ratio: number): PressureLevel {
  if (ratio > OVER_AT) return 'over';
  if (ratio >= TIGHT_AT) return 'tight';
  return 'ok';
}

/**
 * null when there is nothing to judge: no estimate, or status 'done'.
 * A 'doing' task counts its whole estimate, as there is no "remaining" field.
 */
export function taskPressure(
  t: { estimateHours: number; status: string; assignees: string[]; weekendWork: boolean },
  s: number,
  e: number, // day numbers; pass live drag values
  planning: PlanningLike,
  hoursOf: (personId: string) => number, // person's hoursPerDay, or the team default when 0 or unknown
): Pressure | null {
  if (!(t.estimateHours > 0) || t.status === 'done') return null;
  const assumed = t.assignees.length === 0;
  const hours = assumed ? [planning.hoursPerDay] : t.assignees.map(hoursOf);
  const daily = dailyHours(hours, planning.assigneeFactor);
  const days = workDays(s, e, t.weekendWork);
  const capacity = days * daily;
  const estimate = t.estimateHours;
  const neededDays = daily > 0 ? Math.ceil(estimate / daily) : Infinity;
  if (capacity <= 0) {
    return { ratio: Infinity, level: 'over', estimate, capacity: 0, dailyHours: daily, days, neededDays, assumed };
  }
  const ratio = estimate / capacity;
  return { ratio, level: levelOf(ratio), estimate, capacity, dailyHours: daily, days, neededDays, assumed };
}

/** Hours to at most one decimal place, e.g. "7.5h", "36h". */
export function fmtHours(h: number): string {
  return `${Math.round(h * 10) / 10}h`;
}

/** Whole percent, or "no capacity" for a bar with no working time. */
export function fmtRatio(p: Pressure): string {
  return Number.isFinite(p.ratio) ? `${Math.round(p.ratio * 100)}%` : 'no capacity';
}

/** "1 working day", "3 working days" */
export function fmtDays(n: number): string {
  return `${n} working day${n === 1 ? '' : 's'}`;
}

/** Middle-dot separator for labels; built from its code so sources stay ASCII. */
export const SEP = ` ${String.fromCharCode(0xb7)} `;

/** e.g. "40h / 36h, middle dot, 111%" */
export function pressureLabel(p: Pressure): string {
  return `${fmtHours(p.estimate)} / ${fmtHours(p.capacity)}${SEP}${fmtRatio(p)}`;
}
