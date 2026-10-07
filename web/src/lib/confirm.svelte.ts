// Two-click confirmation for destructive actions. window.confirm() is not
// used: some embedded browsers suppress it and return false immediately.
//
// The first hit(key) arms that key and returns false; a second hit(key)
// within the timeout returns true. Hitting a different key re-arms.

export class TwoClick {
  armed = $state<string | null>(null);
  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor(private timeout = 4000) {}

  is(key: string): boolean {
    return this.armed === key;
  }

  hit(key: string): boolean {
    clearTimeout(this.timer);
    if (this.armed === key) {
      this.armed = null;
      return true;
    }
    this.armed = key;
    this.timer = setTimeout(() => (this.armed = null), this.timeout);
    return false;
  }

  reset() {
    clearTimeout(this.timer);
    this.armed = null;
  }
}
