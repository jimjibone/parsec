import { describe, expect, it } from 'vitest';
import { toDay } from './dates';
import { dailyHours, pressureLabel, SEP, taskPressure } from './pressure';

const MON = toDay('2026-01-05');
const WED = MON + 2;
const SUN = MON + 6;
const planning = { hoursPerDay: 6, assigneeFactor: 1 };
const hours: Record<string, number> = { alice: 6, bob: 6, carol: 4 };
const hoursOf = (id: string) => hours[id] ?? planning.hoursPerDay;
const task = (over: Partial<Parameters<typeof taskPressure>[0]> = {}) => ({
  estimateHours: 40,
  status: 'todo',
  assignees: ['alice'],
  weekendWork: false,
  ...over,
});

describe('dailyHours', () => {
  it('splits evenly with factor 1', () => {
    expect(dailyHours([6, 6], 1)).toBe(12);
  });
  it('counts the biggest contributor in full and the rest x factor', () => {
    expect(dailyHours([4, 6, 6], 0.5)).toBe(11);
  });
  it('is 0 with nobody', () => {
    expect(dailyHours([], 1)).toBe(0);
  });
});

describe('taskPressure', () => {
  it('even split across two people', () => {
    const p = taskPressure(task({ assignees: ['alice', 'bob'] }), MON, WED, planning, hoursOf)!;
    expect(p).toMatchObject({ days: 3, dailyHours: 12, capacity: 36, neededDays: 4, level: 'over', assumed: false });
    expect(pressureLabel(p)).toBe(`40h / 36h${SEP}111%`);
  });

  it('factor < 1 raises pressure', () => {
    const p = taskPressure(task({ assignees: ['alice', 'bob'] }), MON, MON + 4, { ...planning, assigneeFactor: 0.5 }, hoursOf)!;
    expect(p.dailyHours).toBe(9);
    expect(p.capacity).toBe(45);
    expect(p.level).toBe('tight');
  });

  it('uses per-person hours', () => {
    const p = taskPressure(task({ estimateHours: 10, assignees: ['carol'] }), MON, WED, planning, hoursOf)!;
    expect(p.capacity).toBe(12);
    expect(p.level).toBe('tight');
  });

  it('assumes one person at the team default when unassigned', () => {
    const p = taskPressure(task({ estimateHours: 12, assignees: [] }), MON, MON + 4, planning, hoursOf)!;
    expect(p).toMatchObject({ assumed: true, dailyHours: 6, capacity: 30, level: 'ok' });
  });

  it('counts weekend days only with weekendWork', () => {
    expect(taskPressure(task(), MON, SUN, planning, hoursOf)!.days).toBe(5);
    expect(taskPressure(task({ weekendWork: true }), MON, SUN, planning, hoursOf)!.days).toBe(7);
  });

  it('is null for done tasks and tasks without an estimate', () => {
    expect(taskPressure(task({ status: 'done' }), MON, WED, planning, hoursOf)).toBeNull();
    expect(taskPressure(task({ estimateHours: 0 }), MON, WED, planning, hoursOf)).toBeNull();
  });

  it('treats exactly full as tight, not over', () => {
    expect(taskPressure(task({ estimateHours: 18 }), MON, WED, planning, hoursOf)!.level).toBe('tight');
  });

  it('reports zero capacity as over, never Infinity%', () => {
    const p = taskPressure(task(), MON + 5, SUN, planning, hoursOf)!; // a weekend only
    expect(p).toMatchObject({ days: 0, capacity: 0, level: 'over', ratio: Infinity });
    expect(pressureLabel(p)).toBe(`40h / 0h${SEP}no capacity`);
  });
});
