// Per-browser view preferences. Kept in localStorage, never in the data repo.

export type View = 'timeline' | 'tracker';
export type Zoom = 'day' | 'week' | 'month';

export const DAY_WIDTH: Record<Zoom, number> = { day: 36, week: 14, month: 4 };

const KEY = 'parsec.ui';

interface Persisted {
  view: View;
  zoom: Zoom;
  collapsed: string[];
  hidden: string[];
  trackerProject: string;
}

function loadPrefs(): Partial<Persisted> {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '{}');
  } catch {
    return {};
  }
}

class UiState {
  view = $state<View>('timeline');
  zoom = $state<Zoom>('day');
  collapsed = $state<string[]>([]);
  hidden = $state<string[]>([]);
  trackerProject = $state(''); // '' = all projects
  selectedTask = $state<string | null>(null);
  gitOpen = $state(false);

  constructor() {
    const p = loadPrefs();
    if (p.view === 'timeline' || p.view === 'tracker') this.view = p.view;
    if (p.zoom && p.zoom in DAY_WIDTH) this.zoom = p.zoom;
    if (Array.isArray(p.collapsed)) this.collapsed = p.collapsed;
    if (Array.isArray(p.hidden)) this.hidden = p.hidden;
    if (typeof p.trackerProject === 'string') this.trackerProject = p.trackerProject;
  }

  persist() {
    const p: Persisted = {
      view: this.view,
      zoom: this.zoom,
      collapsed: this.collapsed,
      hidden: this.hidden,
      trackerProject: this.trackerProject,
    };
    try {
      localStorage.setItem(KEY, JSON.stringify(p));
    } catch {
      // Storage unavailable; preferences just won't stick.
    }
  }

  toggleCollapsed(id: string) {
    this.collapsed = this.collapsed.includes(id) ? this.collapsed.filter((x) => x !== id) : [...this.collapsed, id];
  }

  setHidden(id: string, hidden: boolean) {
    this.hidden = hidden ? [...new Set([...this.hidden, id])] : this.hidden.filter((x) => x !== id);
  }
}

export const ui = new UiState();
