// Dates are handled as integer "day numbers" (days since 1970-01-01 UTC) so
// that arithmetic never trips over time zones or DST.

const MS_PER_DAY = 86_400_000;

export function toDay(iso: string): number {
  const [y, m, d] = iso.split('-').map(Number);
  return Math.round(Date.UTC(y, m - 1, d) / MS_PER_DAY);
}

export function toISO(day: number): string {
  return new Date(day * MS_PER_DAY).toISOString().slice(0, 10);
}

export function today(): number {
  const now = new Date();
  return Math.round(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) / MS_PER_DAY);
}

export function dateOf(day: number): Date {
  return new Date(day * MS_PER_DAY);
}

/** 0 = Monday ... 6 = Sunday */
export function weekday(day: number): number {
  return (dateOf(day).getUTCDay() + 6) % 7;
}

export function startOfMonth(day: number): number {
  const d = dateOf(day);
  return Math.round(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1) / MS_PER_DAY);
}

export function addMonths(day: number, n: number): number {
  const d = dateOf(day);
  return Math.round(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + n, 1) / MS_PER_DAY);
}

export function startOfYear(day: number): number {
  return Math.round(Date.UTC(dateOf(day).getUTCFullYear(), 0, 1) / MS_PER_DAY);
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const WEEKDAYS = ['M', 'T', 'W', 'T', 'F', 'S', 'S'];

export function monthName(day: number): string {
  return MONTHS[dateOf(day).getUTCMonth()];
}

export function weekdayLetter(day: number): string {
  return WEEKDAYS[weekday(day)];
}

export function dayOfMonth(day: number): number {
  return dateOf(day).getUTCDate();
}

export function year(day: number): number {
  return dateOf(day).getUTCFullYear();
}

export function formatShort(iso: string): string {
  if (!iso) return '';
  const d = toDay(iso);
  return `${dayOfMonth(d)} ${monthName(d)}`;
}

export function formatLong(day: number): string {
  return `${dayOfMonth(day)} ${monthName(day)} ${year(day)}`;
}

/** Inclusive length in days. */
export function spanDays(start: string, end: string): number {
  return toDay(end) - toDay(start) + 1;
}
