<script lang="ts">
  import { flushSync, onMount } from 'svelte';
  import { app } from '../state.svelte';
  import { ui, DAY_WIDTH, UNASSIGNED, type Zoom } from '../ui.svelte';
  import Icon from '../Icon.svelte';
  import { TwoClick } from '../confirm.svelte';
  import {
    addMonths,
    dayOfMonth,
    formatLong,
    formatShort,
    monthName,
    startOfMonth,
    startOfYear,
    toDay,
    toISO,
    today,
    weekday,
    weekdayLetter,
    year,
  } from '../dates';
  import { resizeEnd, resizeStart, shiftSpan } from '../schedule';
  import { barLayout, type BarLayout } from './barlayout';
  import { initials } from '../people';
  import type { Milestone, Project, Task } from '../types';
  import { STATUSES } from '../types';

  const SIDEBAR_W = 260;
  const HEAD_H = 48;
  const PROJ_H = 34;
  const LANE_H = 32;
  const BAR_H = 24;
  const BAR_PAD = (LANE_H - BAR_H) / 2;
  const EXTEND = 120; // days added when scrolling near an edge
  const EDGE = 300; // px from edge that triggers extension

  let scroller: HTMLDivElement;
  let canvas: HTMLDivElement;

  let rangeStart = $state(today() - 90);
  let rangeDays = $state(365);
  let scrollX = $state(0);
  let viewW = $state(1200);
  let viewH = $state(800);
  const now = today();

  const dw = $derived(DAY_WIDTH[ui.zoom]);
  const totalW = $derived(rangeDays * dw);
  const chartW = $derived(Math.max(0, viewW - SIDEBAR_W));

  // ---- vertical layout ----

  interface Row {
    project: Project;
    top: number;
    height: number;
    lanes: number;
    collapsed: boolean;
  }

  const layout = $derived.by(() => {
    const rows: Row[] = [];
    let y = 0;
    for (const p of app.projects) {
      if (ui.hidden.includes(p.id)) continue;
      const collapsed = ui.collapsed.includes(p.id);
      // One spare lane at the bottom to drop or create tasks into.
      const lanes = collapsed ? 0 : (app.laneCount.get(p.id) ?? 0) + 1;
      const height = PROJ_H + lanes * LANE_H;
      rows.push({ project: p, top: y, height, lanes, collapsed });
      y += height;
    }
    return { rows, height: y, byProject: new Map(rows.map((r) => [r.project.id, r])) };
  });

  const bodyH = $derived(Math.max(layout.height, viewH - HEAD_H));

  // ---- drag state ----

  type DragKind = 'move' | 'start' | 'end';
  interface Drag {
    kind: DragKind;
    id: string;
    px: number;
    py: number;
    sx0: number;
    sy0: number;
    s0: number;
    e0: number;
    lane0: number;
    s: number;
    e: number;
    dy: number;
    moved: boolean;
    ww: boolean; // task works weekends
  }
  let drag = $state<Drag | null>(null);

  interface LinkDrag {
    from: string;
    x: number;
    y: number;
    over: string | null;
  }
  let link = $state<LinkDrag | null>(null);

  interface Pan {
    px: number;
    py: number;
    sl: number;
    st: number;
    moved: boolean;
  }
  let pan: Pan | null = null;

  interface MsDrag {
    project: string;
    id: string;
    px: number;
    sx0: number;
    d0: number;
    d: number;
    moved: boolean;
  }
  let msDrag = $state<MsDrag | null>(null);

  /** Milestone whose editor popover is open. */
  let msEdit = $state<{ project: string; id: string } | null>(null);

  // ---- task geometry (canvas coordinates) ----

  interface Geo {
    x: number;
    y: number;
    w: number;
    s: number;
    e: number;
  }

  const geo = $derived.by(() => {
    const out = new Map<string, Geo>();
    for (const t of app.tasks) {
      const row = layout.byProject.get(t.projectId);
      if (!row || row.collapsed) continue;
      let s = toDay(t.start);
      let e = toDay(t.end);
      let y = row.top + PROJ_H + (app.displayLane.get(t.id) ?? 0) * LANE_H + BAR_PAD;
      if (drag?.id === t.id) {
        s = drag.s;
        e = drag.e;
        if (drag.kind === 'move') {
          const minY = row.top + PROJ_H + BAR_PAD;
          const maxY = row.top + PROJ_H + (row.lanes - 1) * LANE_H + BAR_PAD;
          y = Math.min(maxY, Math.max(minY, y + drag.dy));
        }
      }
      out.set(t.id, { x: (s - rangeStart) * dw, y, w: (e - s + 1) * dw, s, e });
    }
    return out;
  });

  // ---- dependencies ----

  interface Arrow {
    key: string;
    from: string;
    to: string;
    d: string;
    bad: boolean;
  }

  /** Free px to the right of each bar before the next bar in its row. */
  const labelRoom = $derived.by(() => {
    const rows = new Map<number, { id: string; g: Geo }[]>();
    for (const [id, g] of geo) {
      const r = rows.get(g.y) ?? [];
      r.push({ id, g });
      rows.set(g.y, r);
    }
    const out = new Map<string, number>();
    for (const r of rows.values()) {
      r.sort((a, b) => a.g.x - b.g.x);
      r.forEach((x, i) => {
        const next = r[i + 1];
        if (next) out.set(x.id, next.g.x - (x.g.x + x.g.w) - 24);
      });
    }
    return out;
  });

  const arrows = $derived.by(() => {
    const out: Arrow[] = [];
    for (const t of app.tasks) {
      const b = geo.get(t.id);
      if (!b) continue;
      for (const dep of t.dependsOn) {
        const a = geo.get(dep);
        if (!a) continue;
        out.push({ key: `${dep}>${t.id}`, from: dep, to: t.id, d: depPath(a, b), bad: a.e >= b.s });
      }
    }
    return out;
  });

  function depPath(a: Geo, b: Geo): string {
    const x1 = a.x + a.w;
    const y1 = a.y + BAR_H / 2;
    const x2 = b.x;
    const y2 = b.y + BAR_H / 2;
    if (x2 - x1 >= 16) {
      const xm = x1 + 8;
      return `M${x1},${y1} H${xm} V${y2} H${x2 - 2}`;
    }
    // Route around: out to the right, through the gap next to the target row, in from the left.
    let ym: number;
    if (b.y > a.y) ym = b.y - BAR_PAD;
    else if (b.y < a.y) ym = b.y + BAR_H + BAR_PAD;
    else ym = y1 + BAR_H / 2 + BAR_PAD;
    return `M${x1},${y1} H${x1 + 8} V${ym} H${x2 - 10} V${y2} H${x2 - 2}`;
  }

  // ---- header ticks (only the visible window is rendered) ----

  const vis = $derived({
    from: rangeStart + Math.floor(scrollX / dw) - 2,
    to: rangeStart + Math.ceil((scrollX + chartW) / dw) + 2,
  });

  interface Seg {
    day: number;
    x: number;
    w: number;
    label: string;
    sub?: string;
    weekend?: boolean;
    today?: boolean;
  }

  const ticks = $derived.by(() => {
    const top: Seg[] = [];
    const bottom: Seg[] = [];
    const seg = (a: number, b: number, label: string, extra: Partial<Seg> = {}): Seg => ({
      day: a,
      x: (a - rangeStart) * dw,
      w: (b - a) * dw,
      label,
      ...extra,
    });

    if (ui.zoom === 'month') {
      for (let y = startOfYear(vis.from); y <= vis.to;) {
        const next = addMonths(y, 12);
        top.push(seg(y, next, String(year(y))));
        y = next;
      }
      for (let m = startOfMonth(vis.from); m <= vis.to;) {
        const next = addMonths(m, 1);
        bottom.push(seg(m, next, monthName(m)));
        m = next;
      }
    } else {
      for (let m = startOfMonth(vis.from); m <= vis.to;) {
        const next = addMonths(m, 1);
        top.push(seg(m, next, `${monthName(m)} ${year(m)}`));
        m = next;
      }
      if (ui.zoom === 'day') {
        for (let d = vis.from; d <= vis.to; d++) {
          bottom.push(seg(d, d + 1, String(dayOfMonth(d)), { sub: weekdayLetter(d), weekend: weekday(d) >= 5, today: d === now }));
        }
      } else {
        for (let d = vis.from - weekday(vis.from); d <= vis.to; d += 7) {
          bottom.push(seg(d, d + 7, String(dayOfMonth(d)), { today: now >= d && now < d + 7 }));
        }
      }
    }
    return { top, bottom };
  });

  const weekends = $derived.by(() => {
    if (ui.zoom === 'month') return [];
    const out: number[] = [];
    for (let d = vis.from; d <= vis.to; d++) if (weekday(d) >= 5) out.push((d - rangeStart) * dw);
    return out;
  });

  // ---- scrolling ----

  function onScroll() {
    const el = scroller;
    if (el.scrollLeft < EDGE) {
      const keep = el.scrollLeft;
      rangeStart -= EXTEND;
      rangeDays += EXTEND;
      flushSync();
      el.scrollLeft = keep + EXTEND * dw;
    } else if (el.scrollLeft + el.clientWidth > el.scrollWidth - EDGE) {
      rangeDays += EXTEND;
    }
    scrollX = el.scrollLeft;
  }

  /** Scroll so `day` sits at `frac` of the chart width. */
  function scrollToDay(day: number, frac = 0.3, smooth = false) {
    const pad = Math.ceil((chartW * 1.5) / dw) + 30;
    if (day - pad < rangeStart) {
      const add = rangeStart - (day - pad);
      const keep = scroller.scrollLeft;
      rangeStart -= add;
      rangeDays += add;
      flushSync();
      scroller.scrollLeft = keep + add * dw;
    }
    if (day + pad > rangeStart + rangeDays) {
      rangeDays = day + pad - rangeStart;
      flushSync();
    }
    const left = (day - rangeStart) * dw - chartW * frac + dw / 2;
    scroller.scrollTo({ left, behavior: smooth ? 'smooth' : 'instant' });
    scrollX = scroller.scrollLeft;
  }

  function centerDay(): number {
    return rangeStart + (scrollX + chartW / 2) / dw;
  }

  function setZoom(z: Zoom) {
    if (z === ui.zoom) return;
    const c = centerDay();
    ui.zoom = z;
    flushSync();
    scrollToDay(Math.round(c), 0.5);
  }

  onMount(() => {
    const ro = new ResizeObserver(() => {
      viewW = scroller.clientWidth;
      viewH = scroller.clientHeight;
    });
    ro.observe(scroller);
    viewW = scroller.clientWidth;
    viewH = scroller.clientHeight;
    scrollToDay(now, 0.3);
    return () => ro.disconnect();
  });

  // ---- pointer interaction ----

  function canvasPoint(ev: PointerEvent | MouseEvent) {
    const r = canvas.getBoundingClientRect();
    return { x: ev.clientX - r.left, y: ev.clientY - r.top };
  }

  function startDrag(ev: PointerEvent, t: Task, kind: DragKind) {
    if (ev.button !== 0) return;
    ev.stopPropagation();
    ev.preventDefault();
    const g = geo.get(t.id);
    if (!g) return;
    drag = {
      kind,
      id: t.id,
      px: ev.clientX,
      py: ev.clientY,
      sx0: scroller.scrollLeft,
      sy0: scroller.scrollTop,
      s0: g.s,
      e0: g.e,
      lane0: app.displayLane.get(t.id) ?? 0,
      s: g.s,
      e: g.e,
      dy: 0,
      moved: false,
      ww: t.weekendWork,
    };
  }

  function startLink(ev: PointerEvent, t: Task) {
    if (ev.button !== 0) return;
    ev.stopPropagation();
    ev.preventDefault();
    const p = canvasPoint(ev);
    link = { from: t.id, x: p.x, y: p.y, over: null };
  }

  function startMsDrag(ev: PointerEvent, p: Project, m: Milestone) {
    if (ev.button !== 0) return;
    ev.stopPropagation();
    ev.preventDefault();
    const d = toDay(m.date);
    msDrag = { project: p.id, id: m.id, px: ev.clientX, sx0: scroller.scrollLeft, d0: d, d, moved: false };
  }

  function startPan(ev: PointerEvent) {
    if (ev.button !== 0 || (ev.target as Element).closest('[data-task-id], .milestone, .ms-pop, .dep')) return;
    pan = { px: ev.clientX, py: ev.clientY, sl: scroller.scrollLeft, st: scroller.scrollTop, moved: false };
  }

  function autoScroll(ev: PointerEvent) {
    const r = scroller.getBoundingClientRect();
    const zone = 40;
    if (ev.clientX < r.left + SIDEBAR_W + zone) scroller.scrollLeft -= dw;
    else if (ev.clientX > r.right - zone) scroller.scrollLeft += dw;
  }

  function onPointerMove(ev: PointerEvent) {
    if (drag) {
      const dx = ev.clientX - drag.px + (scroller.scrollLeft - drag.sx0);
      const dy = ev.clientY - drag.py + (scroller.scrollTop - drag.sy0);
      if (!drag.moved && Math.hypot(dx, dy) < 4) return;
      drag.moved = true;
      const dd = Math.round(dx / dw);
      const span =
        drag.kind === 'move'
          ? shiftSpan(drag.s0, drag.e0, dd, drag.ww)
          : drag.kind === 'start'
            ? resizeStart(drag.s0, drag.e0, dd, drag.ww)
            : resizeEnd(drag.s0, drag.e0, dd, drag.ww);
      drag.s = span.s;
      drag.e = span.e;
      if (drag.kind === 'move') drag.dy = dy;
      autoScroll(ev);
    } else if (msDrag) {
      const dx = ev.clientX - msDrag.px + (scroller.scrollLeft - msDrag.sx0);
      if (!msDrag.moved && Math.abs(dx) < 4) return;
      msDrag.moved = true;
      msDrag.d = msDrag.d0 + Math.round(dx / dw);
      autoScroll(ev);
    } else if (link) {
      const p = canvasPoint(ev);
      link.x = p.x;
      link.y = p.y;
      const over = (document.elementFromPoint(ev.clientX, ev.clientY) as Element | null)?.closest<HTMLElement>('[data-task-id]');
      link.over = over && over.dataset.taskId !== link.from ? over.dataset.taskId! : null;
      autoScroll(ev);
    } else if (pan) {
      const dx = ev.clientX - pan.px;
      const dy = ev.clientY - pan.py;
      if (!pan.moved && Math.hypot(dx, dy) < 4) return;
      pan.moved = true;
      scroller.scrollLeft = pan.sl - dx;
      scroller.scrollTop = pan.st - dy;
    }
  }

  function onPointerUp() {
    if (drag) {
      const d = drag;
      drag = null;
      const t = app.taskById.get(d.id);
      if (!t) return;
      if (!d.moved) {
        if (d.kind === 'move') ui.selectedTask = t.id;
        return;
      }
      let target: number | undefined;
      if (d.kind === 'move') {
        const row = layout.byProject.get(t.projectId);
        const lanes = row?.lanes ?? 1;
        target = Math.min(lanes - 1, Math.max(0, d.lane0 + Math.round(d.dy / LANE_H)));
        if (target === d.lane0) target = undefined;
      }
      const next = { ...t, start: toISO(d.s), end: toISO(d.e) };
      if (next.start !== t.start || next.end !== t.end || target !== undefined) app.saveTask(next, target);
    } else if (msDrag) {
      const d = msDrag;
      msDrag = null;
      const m = app.projectById.get(d.project)?.milestones.find((x) => x.id === d.id);
      if (!m) return;
      if (!d.moved) {
        msEdit = msEdit?.id === m.id ? null : { project: d.project, id: m.id };
        return;
      }
      if (d.d !== d.d0) app.saveMilestone(d.project, { ...m, date: toISO(d.d) });
    } else if (link) {
      const l = link;
      link = null;
      if (l.over) app.addDependency(l.over, l.from);
    } else if (pan) {
      const p = pan;
      pan = null;
      if (!p.moved) {
        ui.selectedTask = null;
        msEdit = null;
        confirmArrow.reset();
      }
    }
  }

  // Clicking an arrow arms it; clicking it again removes the dependency.
  const confirmArrow = new TwoClick();

  function removeArrow(a: Arrow) {
    if (confirmArrow.hit(a.key)) {
      app.removeDependency(a.to, a.from);
      return;
    }
    const f = app.taskById.get(a.from)?.title;
    const t = app.taskById.get(a.to)?.title;
    app.toast(`Click the arrow again to remove "${f}" -> "${t}".`, 'info');
  }

  async function onCanvasDblClick(ev: MouseEvent) {
    if ((ev.target as Element).closest('[data-task-id], .milestone, .ms-pop')) return;
    const { x, y } = canvasPoint(ev);
    const row = layout.rows.find((r) => y >= r.top && y < r.top + r.height);
    if (!row) return;
    const day = rangeStart + Math.floor(x / dw);
    // The project header strip holds milestones; the lanes below hold tasks.
    if (y < row.top + PROJ_H) {
      addMilestoneTo(row.project, day);
      return;
    }
    if (row.collapsed) return;
    const lane = Math.floor((y - row.top - PROJ_H) / LANE_H);
    const len = ui.zoom === 'day' ? 3 : ui.zoom === 'week' ? 7 : 14;
    const t = await app.createTask(row.project.id, { start: toISO(day), end: toISO(day + len - 1), lane });
    if (t) ui.selectedTask = t.id;
  }

  // Hidden projects are skipped so each move visibly changes the order.
  function moveProject(id: string, dir: -1 | 1) {
    app.moveProject(
      id,
      dir,
      layout.rows.map((r) => r.project.id),
    );
  }

  async function addTaskTo(p: Project) {
    if (ui.collapsed.includes(p.id)) ui.toggleCollapsed(p.id);
    const start = Math.round(centerDay()) - 1;
    const t = await app.createTask(p.id, { start: toISO(start), end: toISO(start + 2), lane: app.laneCount.get(p.id) ?? 0 });
    if (t) ui.selectedTask = t.id;
  }

  // ---- milestones ----

  async function addMilestoneTo(p: Project, day = Math.round(centerDay())) {
    const m = await app.addMilestone(p.id, 'Milestone', toISO(day));
    if (m) msEdit = { project: p.id, id: m.id };
  }

  const msEditing = $derived.by(() => {
    if (!msEdit) return null;
    const p = app.projectById.get(msEdit.project);
    const m = p?.milestones.find((x) => x.id === msEdit!.id);
    const row = p && layout.byProject.get(p.id);
    return p && m && row ? { p, m, row } : null;
  });

  const confirmMs = new TwoClick();

  function deleteMilestone(p: Project, m: Milestone) {
    if (!confirmMs.hit(m.id)) return;
    msEdit = null;
    app.deleteMilestone(p.id, m.id);
  }

  function renameMilestone(p: Project, m: Milestone, name: string) {
    name = name.trim();
    if (name && name !== m.name) app.saveMilestone(p.id, { ...m, name });
  }

  function onKey(ev: KeyboardEvent) {
    const target = ev.target as HTMLElement;
    if (ev.key === 'Escape' && msEdit && target.closest('.ms-pop')) {
      msEdit = null;
      return;
    }
    if (target.closest('input, textarea, select, [contenteditable]')) return;
    if (ev.key === 'Escape') {
      drag = null;
      link = null;
      msDrag = null;
      msEdit = null;
      return;
    }
    const t = ui.selectedTask ? app.taskById.get(ui.selectedTask) : undefined;
    if (!t || !ev.altKey) return;
    const lane = app.displayLane.get(t.id) ?? 0;
    if (ev.key === 'ArrowUp' && lane > 0) {
      ev.preventDefault();
      app.saveTask(t, lane - 1);
    } else if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      app.saveTask(t, lane + 1);
    } else if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      ev.preventDefault();
      const d = ev.key === 'ArrowLeft' ? -1 : 1;
      const span = shiftSpan(toDay(t.start), toDay(t.end), d, t.weekendWork);
      app.saveTask({ ...t, start: toISO(span.s), end: toISO(span.e) });
    }
  }

  // ---- sidebar ----

  let renaming = $state<string | null>(null);
  let newProject = $state<string | null>(null);
  let hiddenOpen = $state(false);

  async function createProject() {
    const name = newProject?.trim();
    newProject = null;
    if (name) await app.createProject(name);
  }

  function rename(p: Project, name: string) {
    renaming = null;
    name = name.trim();
    if (name && name !== p.name) app.saveProject({ ...p, name });
  }

  function focusSelect(el: HTMLInputElement) {
    el.focus();
    el.select();
  }

  const hiddenProjects = $derived(app.projects.filter((p) => ui.hidden.includes(p.id)));

  function summary(p: Project) {
    const ts = app.tasks.filter((t) => t.projectId === p.id);
    if (!ts.length) return null;
    const s = Math.min(...ts.map((t) => toDay(t.start)));
    const e = Math.max(...ts.map((t) => toDay(t.end)));
    const done = ts.filter((t) => t.status === 'done').length;
    return { x: (s - rangeStart) * dw, w: (e - s + 1) * dw, pct: done / ts.length };
  }

  function barTitle(t: Task): string {
    const st = STATUSES.find((s) => s.id === t.status)?.label ?? t.status;
    const who = t.assignees
      .map((a) => app.personById.get(a)?.name)
      .filter(Boolean)
      .join(', ');
    return `${t.title}\n${formatShort(t.start)} - ${formatShort(t.end)} (${st})${who ? `\n${who}` : ''}`;
  }

  const names = (ids: string[]) => ids.map((a) => app.personById.get(a)?.name ?? a).join(', ');

  // ---- person highlight ----

  /** Active highlight, ignoring a stale id for a person who was removed. */
  const highlight = $derived(ui.highlight === UNASSIGNED || app.personById.has(ui.highlight) ? ui.highlight : '');

  function matches(t: Task | undefined): boolean {
    if (!highlight || !t) return true;
    return highlight === UNASSIGNED ? t.assignees.length === 0 : t.assignees.includes(highlight);
  }

  const highlightCount = $derived(highlight ? app.tasks.filter((t) => matches(t)).length : 0);
</script>

<svelte:window onpointermove={onPointerMove} onpointerup={onPointerUp} onkeydown={onKey} />

<div class="timeline" class:dragging={drag?.moved || msDrag?.moved || link}>
  <div class="toolbar">
    <button onclick={() => scrollToDay(now, 0.3, true)}><Icon name="today" /> Today</button>
    <div class="seg">
      {#each ['day', 'week', 'month'] as const as z (z)}
        <button class:active={ui.zoom === z} onclick={() => setZoom(z)}>{z[0].toUpperCase() + z.slice(1)}</button>
      {/each}
    </div>
    <button class="ghost" onclick={() => (ui.collapsed = [])}>Expand all</button>
    <button class="ghost" onclick={() => (ui.collapsed = app.projects.map((p) => p.id))}>Collapse all</button>
    <div class="highlight" class:active={!!highlight}>
      {#if highlight && highlight !== UNASSIGNED}
        <span class="dot" style:background={app.colorOf(highlight)}></span>
      {/if}
      <select bind:value={ui.highlight} title="Fade tasks not assigned to this person">
        <option value="">Highlight: everyone</option>
        {#each app.people as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
        <option value={UNASSIGNED}>Unassigned</option>
      </select>
      {#if highlight}
        <span class="muted hl-count">{highlightCount} task{highlightCount === 1 ? '' : 's'}</span>
        <button class="ghost icon-btn" onclick={() => (ui.highlight = '')} aria-label="Clear highlight"><Icon name="x" size={14} /></button>
      {/if}
    </div>
    {#if hiddenProjects.length}
      <div class="menu-wrap">
        <button class="ghost" onclick={() => (hiddenOpen = !hiddenOpen)}><Icon name="eyeOff" /> {hiddenProjects.length} hidden</button>
        {#if hiddenOpen}
          <div class="menu">
            {#each hiddenProjects as p (p.id)}
              <button
                class="ghost"
                onclick={() => {
                  ui.setHidden(p.id, false);
                  if (hiddenProjects.length <= 1) hiddenOpen = false;
                }}
              >
                <span class="dot" style:background={p.color}></span>{p.name}<Icon name="eye" />
              </button>
            {/each}
            <button
              class="ghost"
              onclick={() => {
                ui.hidden = [];
                hiddenOpen = false;
              }}>Show all</button
            >
          </div>
        {/if}
      </div>
    {/if}
    <div class="spacer"></div>
    <span class="hint muted"
      >Double-click a lane to add a task, or a project's top strip to add a milestone. Drag the dot on a bar's right edge to link.
      Alt+arrows move the selected task.</span
    >
  </div>

  <div class="scroller" bind:this={scroller} onscroll={onScroll}>
    <div class="inner" style:width="{SIDEBAR_W + totalW}px">
      <div class="head-row" style:height="{HEAD_H}px">
        <!-- corner -->
        <div class="corner">
          {#if newProject !== null}
            <input
              type="text"
              placeholder="Project name"
              bind:value={newProject}
              use:focusSelect
              onkeydown={(e) => {
                if (e.key === 'Enter') createProject();
                if (e.key === 'Escape') newProject = null;
              }}
              onblur={createProject}
            />
          {:else}
            <span class="muted">Projects</span>
            <button class="ghost" onclick={() => (newProject = '')}><Icon name="plus" /> Project</button>
          {/if}
        </div>

        <!-- time header -->
        <div class="header" style:width="{totalW}px">
          {#each ticks.top as s (s.day)}
            <div class="tick top" style:left="{s.x}px" style:width="{s.w}px"><span>{s.label}</span></div>
          {/each}
          {#each ticks.bottom as s (s.day)}
            <div class="tick bottom" class:weekend={s.weekend} class:today={s.today} style:left="{s.x}px" style:width="{s.w}px">
              {#if s.w >= 18}
                <span
                  >{s.label}{#if s.sub && dw >= 30}<small>{s.sub}</small>{/if}</span
                >
              {/if}
            </div>
          {/each}
          {#if now >= rangeStart && now < rangeStart + rangeDays}
            <div class="today-mark" style:left="{(now - rangeStart) * dw + dw / 2}px"></div>
          {/if}
        </div>
      </div>

      <div class="body-row" style:height="{bodyH}px">
        <!-- sidebar -->
        <div class="sidebar">
          {#each layout.rows as r, ri (r.project.id)}
            {@const p = r.project}
            {@const count = app.tasks.filter((t) => t.projectId === p.id).length}
            <div class="proj" style:top="{r.top}px" style:height="{r.height}px">
              <div class="proj-head" style:height="{PROJ_H}px">
                <button class="ghost icon-btn" onclick={() => ui.toggleCollapsed(p.id)} aria-label={r.collapsed ? 'Expand' : 'Collapse'}>
                  <Icon name={r.collapsed ? 'chevronRight' : 'chevronDown'} />
                </button>
                <span class="dot" style:background={p.color}></span>
                {#if renaming === p.id}
                  <input
                    type="text"
                    value={p.name}
                    use:focusSelect
                    onkeydown={(e) => {
                      if (e.key === 'Enter') rename(p, e.currentTarget.value);
                      if (e.key === 'Escape') renaming = null;
                    }}
                    onblur={(e) => rename(p, e.currentTarget.value)}
                  />
                {:else}
                  <span class="name" ondblclick={() => (renaming = p.id)} title="Double-click to rename" role="button" tabindex="-1"
                    >{p.name}</span
                  >
                  <span class="count muted">{count}</span>
                {/if}
                <div class="proj-actions">
                  <button class="ghost icon-btn" disabled={ri === 0} onclick={() => moveProject(p.id, -1)} title="Move project up"
                    ><Icon name="up" size={14} /></button
                  >
                  <button
                    class="ghost icon-btn"
                    disabled={ri === layout.rows.length - 1}
                    onclick={() => moveProject(p.id, 1)}
                    title="Move project down"><Icon name="down" size={14} /></button
                  >
                  <button class="ghost icon-btn" onclick={() => addTaskTo(p)} title="Add task"><Icon name="plus" /></button>
                  <button class="ghost icon-btn" onclick={() => addMilestoneTo(p)} title="Add milestone"
                    ><Icon name="diamond" size={14} /></button
                  >
                  <button class="ghost icon-btn" onclick={() => ui.setHidden(p.id, true)} title="Hide project"
                    ><Icon name="eyeOff" /></button
                  >
                </div>
              </div>
            </div>
          {/each}
          {#if !app.projects.length}
            <div class="empty muted">No projects yet. Create one with the button above.</div>
          {/if}
        </div>

        <!-- chart canvas -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="canvas" style:width="{totalW}px" bind:this={canvas} onpointerdown={startPan} ondblclick={onCanvasDblClick}>
          {#each weekends as x (x)}
            <div class="weekend" style:left="{x}px" style:width="{dw}px"></div>
          {/each}
          {#each ticks.bottom as s (s.day)}
            <div class="vline" style:left="{s.x}px"></div>
          {/each}
          {#each ticks.top as s (s.day)}
            <div class="vline major" style:left="{s.x}px"></div>
          {/each}

          {#each layout.rows as r, i (r.project.id)}
            {@const p = r.project}
            <div class="band" class:alt={i % 2 === 1} style:top="{r.top}px" style:height="{r.height}px"></div>
            {#if r.collapsed}
              {@const sm = summary(p)}
              {#if sm}
                <div
                  class="summary"
                  style:left="{sm.x}px"
                  style:width="{sm.w}px"
                  style:top="{r.top + PROJ_H / 2 - 4}px"
                  style:--c={p.color}
                >
                  <div class="fill" style:width="{sm.pct * 100}%"></div>
                </div>
              {/if}
            {/if}
            {#each p.milestones as m (m.id)}
              {@const md = msDrag?.id === m.id ? msDrag.d : toDay(m.date)}
              {@const mx = (md - rangeStart) * dw + dw / 2}
              <div class="ms-line" style:left="{mx}px" style:top="{r.top}px" style:height="{r.height}px" style:--c={p.color}></div>
              <div
                class="milestone"
                class:dragging={msDrag?.id === m.id && msDrag.moved}
                class:selected={msEdit?.id === m.id}
                style:left="{mx}px"
                style:top="{r.top + PROJ_H / 2}px"
                style:--c={p.color}
                title="{m.name}: {formatLong(md)}{msDrag?.id === m.id ? '' : '\nDrag to move, click to edit.'}"
                onpointerdown={(e) => startMsDrag(e, p, m)}
                role="button"
                tabindex="-1"
              >
                <span class="diamond"></span>
                <span class="ms-label">{m.name}</span>
                {#if msDrag?.id === m.id && msDrag.moved}
                  <span class="ms-date">{formatShort(toISO(md))}</span>
                {/if}
              </div>
            {/each}
          {/each}

          {#if msEditing}
            {@const { p, m, row } = msEditing}
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
              class="ms-pop"
              style:left="{(toDay(m.date) - rangeStart) * dw + dw / 2}px"
              style:top="{row.top + PROJ_H - 4}px"
              style:--c={p.color}
              onpointerdown={(e) => e.stopPropagation()}
            >
              {#key m.id}
                <input
                  type="text"
                  value={m.name}
                  aria-label="Milestone name"
                  use:focusSelect
                  onkeydown={(e) => {
                    if (e.key === 'Enter') {
                      renameMilestone(p, m, e.currentTarget.value);
                      msEdit = null;
                    }
                    // Revert first so a blur on close doesn't save the edit.
                    if (e.key === 'Escape') e.currentTarget.value = m.name;
                  }}
                  onblur={(e) => renameMilestone(p, m, e.currentTarget.value)}
                />
              {/key}
              <input
                type="date"
                value={m.date}
                aria-label="Milestone date"
                onchange={(e) => {
                  const v = e.currentTarget.value;
                  if (v) app.saveMilestone(p.id, { ...m, date: v });
                }}
              />
              <button
                class="ghost icon-btn"
                class:danger={confirmMs.is(m.id)}
                onclick={() => deleteMilestone(p, m)}
                title={confirmMs.is(m.id) ? 'Click again to delete' : 'Delete milestone'}><Icon name="trash" size={14} /></button
              >
              <button class="ghost icon-btn" onclick={() => (msEdit = null)} aria-label="Close"><Icon name="x" size={14} /></button>
            </div>
          {/if}

          {#if now >= rangeStart && now < rangeStart + rangeDays}
            <div class="today-line" style:left="{(now - rangeStart) * dw + dw / 2}px"></div>
          {/if}

          <svg class="deps" width={totalW} height={bodyH}>
            <defs>
              <marker id="arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" class="head" />
              </marker>
              <marker id="arrow-bad" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" class="head bad" />
              </marker>
            </defs>
            {#each arrows as a (a.key)}
              <g
                class="dep"
                class:bad={a.bad}
                class:armed={confirmArrow.is(a.key)}
                class:faded={!matches(app.taskById.get(a.from)) && !matches(app.taskById.get(a.to))}
              >
                <path class="hit" d={a.d} onclick={() => removeArrow(a)} role="presentation">
                  <title
                    >{app.taskById.get(a.from)?.title} must finish before {app.taskById.get(a.to)?.title} starts{a.bad
                      ? ' (violated)'
                      : ''}. Click twice to remove.</title
                  >
                </path>
                <path class="line" d={a.d} marker-end={a.bad ? 'url(#arrow-bad)' : 'url(#arrow)'} />
              </g>
            {/each}
            {#if link}
              {@const g = geo.get(link.from)}
              {#if g}
                <path class="link-preview" d="M{g.x + g.w},{g.y + BAR_H / 2} L{link.x},{link.y}" />
              {/if}
            {/if}
          </svg>

          {#snippet pills(t: Task, lay: BarLayout)}
            {#if lay.shown || lay.more}
              <span class="who"
                >{#each t.assignees.slice(0, lay.shown) as a (a)}<span
                    class="av"
                    class:hl={highlight === a}
                    style:background={app.colorOf(a)}>{initials(app.personById.get(a)?.name)}</span
                  >{/each}{#if lay.more}<span class="av more" title={names(t.assignees.slice(lay.shown))}
                    >{lay.shown ? '+' : ''}{lay.more}</span
                  >{/if}</span
              >
            {/if}
          {/snippet}

          {#each app.tasks as t (t.id)}
            {@const g = geo.get(t.id)}
            {#if g}
              {@const p = app.projectById.get(t.projectId)}
              {@const room = labelRoom.get(t.id) ?? Infinity}
              {@const lay = barLayout(g.w, t.title.length, t.assignees.length, room)}
              <div
                class="bar {t.status}"
                class:selected={ui.selectedTask === t.id}
                class:dragging={drag?.id === t.id && drag.moved}
                class:link-target={link?.over === t.id}
                class:faded={!matches(t)}
                data-task-id={t.id}
                style:left="{g.x}px"
                style:top="{g.y}px"
                style:width="{g.w}px"
                style:height="{BAR_H}px"
                style:--c={p?.color ?? 'var(--accent)'}
                title={barTitle(t)}
                onpointerdown={(e) => startDrag(e, t, 'move')}
                role="button"
                tabindex="-1"
              >
                {#if !t.weekendWork}
                  <!-- Repeating 7-day pattern; first band starts on the first Saturday in the bar. -->
                  <div
                    class="weekend-off"
                    style:background-size="{7 * dw}px 100%"
                    style:background-position-x="{((5 - weekday(g.s) + 7) % 7) * dw}px"
                    style:--wk="{2 * dw}px"
                  ></div>
                {/if}
                <div class="handle l" onpointerdown={(e) => startDrag(e, t, 'start')} role="presentation"></div>
                {#if lay.titleInside}
                  <span class="label">{t.title}</span>
                {/if}
                {#if lay.pillsInside}
                  {@render pills(t, lay)}
                {/if}
                <div class="handle r" onpointerdown={(e) => startDrag(e, t, 'end')} role="presentation"></div>
                <div
                  class="link-dot"
                  onpointerdown={(e) => startLink(e, t)}
                  title="Drag to another task to add a dependency"
                  role="presentation"
                ></div>
              </div>
              {#if !lay.titleInside && room >= 30}
                <span
                  class="outside-label"
                  class:faded={!matches(t)}
                  style:left="{g.x + g.w + 18}px"
                  style:top="{g.y}px"
                  style:height="{BAR_H}px"
                  style:max-width={room === Infinity ? null : `${room}px`}
                >
                  {#if !lay.pillsInside}{@render pills(t, lay)}{/if}
                  <span class="ot">{t.title}</span>
                </span>
              {/if}
            {/if}
          {/each}
        </div>
      </div>
    </div>
  </div>
</div>

<style>
  .timeline {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .timeline.dragging {
    cursor: grabbing;
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    flex: none;
  }

  .seg {
    display: flex;
  }

  .seg button {
    border-radius: 0;
    margin-left: -1px;
  }

  .seg button:first-child {
    border-radius: var(--radius) 0 0 var(--radius);
  }

  .seg button:last-child {
    border-radius: 0 var(--radius) var(--radius) 0;
  }

  .seg button.active {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent);
    position: relative;
  }

  .spacer {
    flex: 1;
  }

  .hint {
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .menu-wrap {
    position: relative;
  }

  .menu {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 20;
    display: flex;
    flex-direction: column;
    min-width: 200px;
    padding: 4px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: var(--shadow);
  }

  .menu button {
    justify-content: flex-start;
  }

  .scroller {
    flex: 1;
    min-height: 0;
    overflow: auto;
    position: relative;
    overscroll-behavior-x: contain;
  }

  /* Sticky elements are confined to their parent box, so the sidebar and
     corner sit inside full-width flex rows rather than grid cells. */
  .inner {
    position: relative;
  }

  .head-row {
    position: sticky;
    top: 0;
    z-index: 4;
    display: flex;
  }

  .body-row {
    display: flex;
  }

  .corner {
    position: sticky;
    left: 0;
    z-index: 5;
    width: 260px;
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding: 0 8px 0 12px;
    background: var(--surface);
    border-right: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
  }

  .corner input {
    width: 100%;
  }

  .header {
    position: relative;
    flex: none;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    /* No overflow clipping here: it would stop the month labels sticking. */
    user-select: none;
  }

  .tick {
    position: absolute;
    height: 24px;
    display: flex;
    align-items: center;
    border-left: 1px solid var(--grid-line);
    font-size: 12px;
    white-space: nowrap;
  }

  .tick.top {
    top: 0;
    font-weight: 600;
  }

  .tick.top span {
    position: sticky;
    left: calc(260px + 6px);
    padding: 0 6px;
  }

  .tick.bottom {
    top: 24px;
    justify-content: center;
    color: var(--text-2);
  }

  .tick.bottom span {
    display: flex;
    gap: 3px;
    align-items: baseline;
  }

  .tick.bottom small {
    font-size: 10px;
    color: var(--text-3);
  }

  .tick.weekend {
    background: var(--weekend);
  }

  .tick.today {
    color: var(--today);
    font-weight: 700;
  }

  .today-mark {
    position: absolute;
    bottom: 0;
    width: 8px;
    height: 8px;
    margin-left: -4px;
    background: var(--today);
    border-radius: 50%;
    transform: translateY(50%);
  }

  .sidebar {
    position: sticky;
    left: 0;
    z-index: 4;
    width: 260px;
    flex: none;
    background: var(--surface);
    border-right: 1px solid var(--border);
  }

  .proj {
    position: absolute;
    left: 0;
    right: 0;
    border-bottom: 1px solid var(--border);
  }

  .proj-head {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px 0 4px;
  }

  .proj-head .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: default;
  }

  .proj-head input {
    flex: 1;
    padding: 2px 6px;
  }

  .count {
    font-size: 12px;
  }

  /* Overlays the end of the header on hover instead of reserving width,
     so project names use the full sidebar when not hovered. */
  .proj-actions {
    position: absolute;
    top: 0;
    bottom: 0;
    right: 6px;
    display: flex;
    align-items: center;
    padding-left: 16px;
    background: linear-gradient(to right, transparent, var(--surface) 16px);
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.1s;
  }

  .proj:hover .proj-actions,
  .proj-actions:focus-within {
    opacity: 1;
    pointer-events: auto;
  }

  .sidebar .empty {
    padding: 16px;
    font-size: 13px;
  }

  .canvas {
    flex: none;
    position: relative;
    overflow: hidden;
    user-select: none;
    cursor: grab;
  }

  .weekend {
    position: absolute;
    top: 0;
    bottom: 0;
    background: var(--weekend);
    pointer-events: none;
  }

  .vline {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: var(--grid-line);
    pointer-events: none;
  }

  .vline.major {
    background: var(--border-strong);
    opacity: 0.6;
  }

  .band {
    position: absolute;
    left: 0;
    right: 0;
    border-bottom: 1px solid var(--border);
    pointer-events: none;
  }

  .band.alt {
    background: var(--band);
  }

  .today-line {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 2px;
    margin-left: -1px;
    background: var(--today);
    opacity: 0.7;
    pointer-events: none;
    z-index: 1;
  }

  .summary {
    position: absolute;
    height: 8px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--c) 30%, transparent);
    overflow: hidden;
    pointer-events: none;
  }

  .summary .fill {
    height: 100%;
    background: var(--c);
  }

  .ms-line {
    position: absolute;
    width: 0;
    border-left: 1px dashed var(--c);
    opacity: 0.6;
    pointer-events: none;
  }

  .milestone {
    position: absolute;
    display: flex;
    align-items: center;
    gap: 6px;
    transform: translate(-7px, -50%);
    z-index: 2;
    cursor: grab;
    touch-action: none;
  }

  .milestone.dragging {
    z-index: 6;
    cursor: grabbing;
  }

  .milestone.selected .diamond,
  .milestone:hover .diamond {
    box-shadow: 0 0 0 2px var(--c);
  }

  .ms-date {
    font-size: 11px;
    color: var(--text-2);
    white-space: nowrap;
  }

  .ms-pop {
    position: absolute;
    z-index: 7;
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px;
    transform: translateX(-12px);
    background: var(--surface);
    border: 1px solid var(--border);
    border-top: 2px solid var(--c);
    border-radius: 8px;
    box-shadow: var(--shadow);
    cursor: default;
    user-select: text;
  }

  .ms-pop input[type='text'] {
    width: 160px;
  }

  .ms-pop .danger {
    color: var(--danger);
  }

  .diamond {
    width: 14px;
    height: 14px;
    flex: none;
    background: var(--c);
    transform: rotate(45deg) scale(0.8);
    border: 2px solid var(--surface);
    box-shadow: 0 0 0 1px var(--c);
  }

  .ms-label {
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
    padding: 0 4px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--surface) 85%, transparent);
  }

  .deps {
    position: absolute;
    left: 0;
    top: 0;
    pointer-events: none;
    z-index: 2;
    overflow: visible;
  }

  .dep .line {
    fill: none;
    stroke: var(--text-3);
    stroke-width: 1.5;
  }

  .dep .hit {
    fill: none;
    stroke: transparent;
    stroke-width: 10;
    pointer-events: stroke;
    cursor: pointer;
  }

  .dep:hover .line {
    stroke: var(--text);
    stroke-width: 2;
  }

  .dep.bad .line {
    stroke: var(--danger);
  }

  .dep.armed .line {
    stroke: var(--danger);
    stroke-width: 2.5;
    stroke-dasharray: 5 3;
  }

  .head {
    fill: var(--text-3);
  }

  .head.bad {
    fill: var(--danger);
  }

  .link-preview {
    stroke: var(--accent);
    stroke-width: 2;
    stroke-dasharray: 5 4;
    fill: none;
  }

  .bar {
    position: absolute;
    z-index: 3;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 8px;
    border-radius: 5px;
    background: var(--c);
    color: #fff;
    font-size: 12px;
    font-weight: 500;
    cursor: grab;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.18);
    touch-action: none;
  }

  .bar.todo {
    background: color-mix(in srgb, var(--c) 72%, var(--surface));
  }

  .bar.done {
    opacity: 0.5;
  }

  .bar.done .label {
    text-decoration: line-through;
  }

  .bar.blocked {
    background: repeating-linear-gradient(-45deg, var(--c) 0 6px, color-mix(in srgb, var(--c) 75%, #000) 6px 12px);
    outline: 2px solid var(--danger);
    outline-offset: 1px;
  }

  .bar.selected {
    outline: 2px solid var(--text);
    outline-offset: 2px;
  }

  .bar.dragging {
    z-index: 6;
    cursor: grabbing;
    box-shadow: var(--shadow);
    opacity: 0.9;
  }

  .bar.link-target {
    outline: 2px dashed var(--accent);
    outline-offset: 2px;
  }

  /* Fades weekend days inside a bar; label and avatars sit above it. */
  .weekend-off {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
    background-image: linear-gradient(90deg, color-mix(in srgb, var(--surface) 55%, transparent) 0 var(--wk), transparent var(--wk));
    background-repeat: repeat-x;
  }

  .label,
  .who {
    position: relative;
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    text-shadow: 0 1px 1px rgba(0, 0, 0, 0.25);
  }

  /* Pill sizes are mirrored in barlayout.ts. */
  .who {
    display: flex;
    flex: none;
    gap: 2px;
  }

  .bar .who {
    margin-left: auto;
  }

  .av {
    min-width: 18px;
    padding: 1px 3px;
    border-radius: 3px;
    font-size: 9px;
    font-weight: 700;
    line-height: 12px;
    text-align: center;
    color: #fff;
    /* Person pills set their own background inline; this is for "+N". */
    background: rgba(0, 0, 0, 0.22);
    box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.45);
  }

  .av.hl {
    box-shadow: 0 0 0 2px #fff;
  }

  .av.more {
    box-shadow: none;
  }

  .outside-label {
    position: absolute;
    z-index: 3;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    white-space: nowrap;
    pointer-events: none;
    color: var(--text-2);
  }

  .outside-label .av {
    box-shadow: none;
  }

  .outside-label .av.more {
    background: var(--surface-3);
    color: var(--text);
  }

  /* Person highlight: everything not matching fades back. */
  .bar.faded,
  .outside-label.faded {
    opacity: 0.18;
    filter: grayscale(0.7);
  }

  .dep.faded {
    opacity: 0.2;
  }

  .highlight {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .highlight.active select {
    border-color: var(--accent);
  }

  .hl-count {
    font-size: 12px;
    white-space: nowrap;
  }

  .ot {
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .handle {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 8px;
    cursor: ew-resize;
  }

  .handle.l {
    left: -2px;
  }

  .handle.r {
    right: -2px;
  }

  .handle::after {
    content: '';
    position: absolute;
    top: 6px;
    bottom: 6px;
    left: 3px;
    width: 2px;
    border-radius: 1px;
    background: rgba(255, 255, 255, 0.8);
    opacity: 0;
  }

  .bar:hover .handle::after {
    opacity: 1;
  }

  .link-dot {
    position: absolute;
    right: -14px;
    top: 50%;
    width: 10px;
    height: 10px;
    margin-top: -5px;
    border-radius: 50%;
    background: var(--surface);
    border: 2px solid var(--c);
    cursor: crosshair;
    opacity: 0;
  }

  .bar:hover .link-dot,
  .bar.selected .link-dot {
    opacity: 1;
  }
</style>
