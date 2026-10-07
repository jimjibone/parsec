// Decides what goes inside a timeline bar: the title and/or assignee pills.
// Assignees take priority over a full-length title, so short bars still
// show who is on them. Widths are estimates matching the .av / .label CSS.

const BAR_PAD = 8; // .bar horizontal padding
const INNER_GAP = 6; // .bar flex gap
const PILL_W = 18; // .av min-width
const PILL_GAP = 2; // .who flex gap
const CHAR_W = 7; // average title glyph width at 12px
const TITLE_MAX = 200; // never reserve more than this for the title
const TITLE_MIN = 60; // shortest useful truncated title
const LONG_BAR = 160; // inner width at which the title stays inside, truncated
const OUTSIDE_TITLE_MIN = 30; // outside label is hidden below this width (Timeline)

export interface BarLayout {
  titleInside: boolean;
  pillsInside: boolean;
  shown: number; // assignees shown as initials
  more: number; // count in the trailing "+N" pill
}

const plusW = (more: number) => (more < 10 ? PILL_W : 24);

function pillsWidth(shown: number, more: number): number {
  const items = shown + (more ? 1 : 0);
  if (!items) return 0;
  return shown * PILL_W + (more ? plusW(more) : 0) + (items - 1) * PILL_GAP;
}

/** Most pills that fit in `avail` px: all of them, or k initials plus "+rest" (k >= 1). */
export function fitPills(n: number, avail: number): { shown: number; more: number } | null {
  if (pillsWidth(n, 0) <= avail) return { shown: n, more: 0 };
  for (let k = n - 1; k >= 1; k--) {
    if (pillsWidth(k, n - k) <= avail) return { shown: k, more: n - k };
  }
  return null;
}

/**
 * `room` is the free px to the right of the bar (before the next bar in the
 * row), used to decide whether the outside label can hold the pills.
 */
export function barLayout(barWidth: number, titleChars: number, assignees: number, room = Infinity): BarLayout {
  const inner = barWidth - 2 * BAR_PAD;
  const titleW = Math.min(TITLE_MAX, titleChars * CHAR_W);
  const gap = assignees ? INNER_GAP : 0;

  // Everything fits.
  if (titleW + gap + pillsWidth(assignees, 0) <= inner) {
    return { titleInside: true, pillsInside: true, shown: assignees, more: 0 };
  }
  // Long bar: keep a truncated title inside next to the pills.
  if (inner >= LONG_BAR) {
    const f = fitPills(assignees, inner - TITLE_MIN - gap);
    if (f) return { titleInside: true, pillsInside: true, ...f };
  }
  // Title goes outside; pills stay inside if at least one initial fits.
  const f = fitPills(assignees, inner);
  if (f) return { titleInside: false, pillsInside: true, ...f };
  // Bar too short: pills go in front of the outside title, if there is room.
  const shown = Math.min(assignees, 2);
  if (pillsWidth(shown, assignees - shown) + INNER_GAP + OUTSIDE_TITLE_MIN <= room) {
    return { titleInside: false, pillsInside: false, shown, more: assignees - shown };
  }
  // Last resort: a bare count inside the bar (shown = 0 renders without "+").
  if (plusW(assignees) <= inner) return { titleInside: false, pillsInside: true, shown: 0, more: assignees };
  return { titleInside: false, pillsInside: true, shown: 0, more: 0 };
}
