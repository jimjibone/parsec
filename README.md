<p align="center">
  <img src="web/public/favicon.svg" width="72" height="72" alt="parsec logo">
</p>

<h1 align="center">parsec</h1>

Project task planning and timing, with a gantt timeline. Named after the unit of
time used to make the Kessel Run.

Data lives in a plain git repository as YAML files, so history, sync and backup
come from git.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshot-dark.png">
  <img src="docs/screenshot-light.png" alt="parsec timeline: three projects (Millennium Falcon refit, Deflector shields, Trench run) with tasks, dependency arrows, milestones and assignee initials">
</picture>

## Features

- Timeline (gantt) with a swimlane per project; collapse or hide projects.
- Drag a bar to move it, drag its edges to change start/end dates.
- Concurrent tasks stack in rows. Drag a bar up/down (or Alt+Up/Down, or the
  arrows in the task panel) to reorder rows. Dropping onto an occupied row
  inserts a new row.
- Dependencies drawn as arrows (red when the predecessor ends on or after the
  successor starts). Drag the dot on a bar's right edge onto another bar to add
  one; click an arrow to remove it. Cycles are rejected.
- Weekends are non-working days by default: durations count working days,
  moving a task keeps its working-day length, start/end dates skip weekends,
  and weekend days inside a bar are faded. Tick "Works weekends" on a task to
  treat every day as a working day.
- Milestones per project, shown as diamonds with a marker line. Double-click
  a project's top strip (or use the diamond button on its sidebar row) to add
  one, drag a diamond to change its date, and click it to rename, set the date
  or delete it.
- Continuous horizontal scroll (the range grows as you scroll), day/week/month
  zoom, a Today button, and click-drag panning on empty space.
- Assignee initials on bars, in each person's colour (set in the people list;
  new people get the least-used palette colour). Short bars fall back to
  "first one or two + N" or a bare count.
- Highlight a person (or "Unassigned") on the timeline to fade every other
  task.
- Task panel: title, status, project, dates, estimate (hours), assignees,
  dependencies, description.
- Projects & tasks view: sortable, filterable task table, project editor
  (name, colour, description, milestones), people list.
- Git panel: changed files, commit, pull (rebase), push, sync, remote URL,
  history, abort a conflicted rebase.
- Undo/redo: buttons next to the git button, or Cmd+Z / Cmd+Shift+Z
  (Ctrl+Z / Ctrl+Shift+Z / Ctrl+Y elsewhere) outside text fields. Each person
  undoes only their own changes. The server keeps the stack in memory, so a
  restart clears it. In single-user mode commit, pull and sync clear it too.
  If a file was changed by someone else (or outside parsec) since a step,
  undoing that step is refused and the step dropped.
- Shared server mode: sign-in through GitLab (or any OpenID Connect
  provider) or local passwords, viewer/editor/admin roles managed in the
  app, live updates between browsers, conflict detection, and automatic
  commits per person. See [Shared server](#shared-server).
- Light/dark theme follows the system setting.

## Running

Requires Go 1.22+, Node 20+ and git.

```sh
make build
./parsec --data ~/plans
```

Then open http://127.0.0.1:7343.

- `--data` is the data repository. It is created and `git init`ed if missing.
- `--addr` sets the listen address (default `127.0.0.1:7343`). Without a
  config file there is no sign-in; keep it on localhost.
- `--config` reads a config file; see [Shared server](#shared-server).

Git operations use the system `git` binary, so existing SSH keys, credential
helpers and commit signing settings apply. Interactive prompts are disabled;
credentials must be available non-interactively.

## Shared server

One parsec instance on an internal Linux host, behind an HTTPS reverse proxy,
used from any browser. Nothing is installed on users' machines.

### Setup

1. **Data repository.** Create an empty project in GitLab (e.g.
   `team/plans`). On the host, as the service user, clone it to the data
   directory and give that user a deploy key with write access (or an HTTPS
   token in a credential helper), plus a committer identity:

   ```sh
   git clone git@gitlab.example.com:team/plans.git /var/lib/parsec/data
   git -C /var/lib/parsec/data config user.name "parsec"
   git -C /var/lib/parsec/data config user.email "parsec@example.com"
   ```

   Commits are authored by the person who made the change; this identity is
   only the committer.

2. **GitLab application** (for sign-in). As a GitLab admin: Admin >
   Applications > New application. Name `parsec`, redirect URI
   `https://parsec.example.com/auth/callback`, scopes `openid`, `profile`,
   `email`, tick **Trusted** (skips the consent screen) and keep
   **Confidential**. Note the application ID and secret. A group-owned
   application (group > Settings > Applications) also works, without the
   trusted option.

3. **Config.** Copy [deploy/parsec.example.yaml](deploy/parsec.example.yaml)
   to `/etc/parsec/parsec.yaml` and fill in `publicURL`, the GitLab `issuer`,
   `clientID` and your GitLab username under `admins`. Put the secret in
   `/etc/parsec/env` as `PARSEC_OIDC_CLIENT_SECRET=...` (mode 600).

4. **Service and proxy.** Install the binary as `/usr/local/bin/parsec` and
   [deploy/parsec.service](deploy/parsec.service), then put
   [Caddy](deploy/Caddyfile) or [nginx](deploy/nginx.conf) in front. The
   proxy must pass the `Host` header and must not buffer `/api/events`
   (server-sent events).

5. **Access.** Sign in as the admin, open the user menu > Settings, and set
   roles. Others appear there after their first sign-in, with the default
   role (viewer unless changed). Usernames can be added in advance.

### Roles

| Role      | Can                                                                 |
| --------- | ------------------------------------------------------------------- |
| No access | sign in, nothing else                                               |
| Viewer    | see everything; the timeline and task panel are read-only           |
| Editor    | edit projects, tasks, people and milestones; undo their own changes |
| Admin     | editor, plus the git panel and the settings panel                   |

Admins listed in the config file cannot be demoted or removed in the app.

### Without SSO

Set `auth.mode: local` and create accounts from the command line (or later
in the settings panel):

```sh
parsec --config /etc/parsec/parsec.yaml passwd alice admin
```

The password is read from the terminal, or one line of stdin. After 10
failed sign-ins in 15 minutes an account is locked for the rest of that
window.

### How shared editing works

- **Live updates.** Browsers keep an event stream open and refetch when
  someone else saves, so screens do not go stale. A refetch waits until any
  drag in progress ends.
- **Conflicts.** Every task, project and person carries a version. Saving an
  old version that someone else has changed since is refused ("changed by
  Bob in the meantime") and the screen shows the latest data. Quick
  successive saves from one browser tab never conflict with each other.
- **Automatic commits.** Each person's changed files are committed, with them
  as author, once they have not edited for `git.idle` (default 5 minutes).
  The commit message lists what they did, e.g. `Edit task x3, Create
  project`. With `git.push` on, parsec then pulls (rebase) and pushes. A
  pull that conflicts is aborted and reported on the git button; an admin
  resolves it in a clone. Pending changes are committed on shutdown.
  Attribution follows files: if two people edit the same task file before
  either is committed, the first commit carries both edits.
- **Undo** spans these automatic commits; undoing makes a new change that is
  committed in turn.
- **Security.** Sessions are signed cookies (HttpOnly, SameSite=Lax, Secure
  behind https). Writes from other origins are refused. GitLab sign-in uses
  the authorization code flow with PKCE, state and nonce. `stateDir` holds
  `users.yaml` (roles, bcrypt password hashes) and `session.key`; deleting
  `session.key` signs everyone out.

GitLab includes `email` in the ID token only with the `email` scope and when
the user has a public email address; without it, commits use
`<username>@users.parsec.invalid`.

## Development

```sh
make dev-server   # Go API on :7343, data in ./parsec-data
make dev-web      # Vite on :5173, proxies /api to :7343
make check        # go vet + svelte-check
make test         # Go tests
```

## Data layout

```
people.yaml
projects/<projectId>/project.yaml
projects/<projectId>/tasks/<taskId>.yaml
```

One file per task keeps diffs small and limits merge conflicts when several
people sync the same repository. Dates are inclusive `YYYY-MM-DD`. A task's
`lane` is its row within the project swimlane; overlapping lanes in the stored
data (e.g. after a merge) are resolved on display and fixed on the next edit.

View preferences (zoom, collapsed and hidden projects) are stored per browser,
not in the data repository.

## License

[Apache License 2.0](LICENSE).
