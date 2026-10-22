import type { main } from '../wailsjs/go/models'

// The backend's Linear errors (linear.go), matched by their text.
export const LINEAR_NOT_CONNECTED = "Linear isn't connected"
export const LINEAR_REJECTED = 'Linear rejected the saved key'
export const LINEAR_LIMITED = 'Linear is limiting searches, try again in a moment'

export const REPOS_KEY = 'grove.linearRepos' // repo paths ticked the last time work was started

export type Ticket = { kind: 'issue'; issue: main.LinearIssue } | { kind: 'project'; project: main.LinearProject }

// Target is a repo's display name and its worktree dir, as the prompt names them.
export type Target = { repo: string; path: string }

const DONE = ['completed', 'canceled']
const DESCRIPTION_MAX = 600 // per issue in a project prompt: enough to plan from, not a wall

// defaultRepos is the set ticked last time that is still in the window, in the
// window's order; else the current worktree's repo; else the first.
export function defaultRepos(saved: string[], repos: main.RepoInfo[], current: string): string[] {
  const kept = repos.map((r) => r.path).filter((p) => saved.includes(p))
  if (kept.length) return kept
  const cur = repos.find((r) => r.path === current) ?? repos[0]
  return cur ? [cur.path] : []
}

// nextLead keeps the lead while it is ticked, else moves it to the first ticked repo.
export function nextLead(lead: string, ticked: string[]): string {
  return ticked.includes(lead) ? lead : (ticked[0] ?? '')
}

export function slug(s: string): string {
  const cut = (v: string) => v.replace(/^-+|-+$/g, '')
  return cut(cut(s.toLowerCase().replace(/[^a-z0-9]+/g, '-')).slice(0, 60)) || 'work'
}

// suggestBranch is an issue's Linear branch. A project has none, so it borrows
// the prefix Linear gives the viewer's issue branches ("stu/eng-1-x" → "stu").
export function suggestBranch(t: Ticket): string {
  if (t.kind === 'issue') return t.issue.branchName
  const named = (t.project.issues ?? []).find((i) => i.branchName.includes('/'))
  const name = slug(t.project.name)
  return named ? `${named.branchName.split('/')[0]}/${name}` : name
}

export function suggestName(t: Ticket): string {
  return t.kind === 'issue' ? t.issue.id.toLowerCase() : slug(t.project.name)
}

// buildPrompt is the lead Claude's first prompt; targets[0] is the lead.
export function buildPrompt(t: Ticket, targets: Target[], branch: string): string {
  const [lead, ...rest] = targets
  const where =
    rest.length === 0
      ? `You're in the ${lead?.repo} worktree on branch ${branch}.`
      : [
          `You're in the ${lead.repo} worktree. The work spans ${targets.length} repos, each with a worktree on branch ${branch}:`,
          `- ${lead.repo} (here): ${lead.path}`,
          ...rest.map((r) => `- ${r.repo}: ${r.path}`),
        ].join('\n')
  const sections =
    t.kind === 'issue'
      ? [
          `Plan the work for Linear issue ${t.issue.id}: ${t.issue.title}\n${t.issue.url}`,
          where,
          rest.length ? 'Plan across every repo before changing anything.' : '',
          t.issue.description ? `## Description\n${t.issue.description}` : '',
        ]
      : [
          `Make an end-to-end plan for the Linear project "${t.project.name}".\n${t.project.url}`,
          where,
          'Cover the whole stack: what changes in each repo, the contracts between them, and the order to ship in. Map each step to the issues below.',
          t.project.description || t.project.content ? ['## Project', [t.project.description, t.project.content].filter(Boolean).join('\n\n')].join('\n') : '',
          t.project.issues?.length
            ? ["## Issues (done ones are already shipped: build on them, don't plan them again)", ...t.project.issues.map(issueLine)].join('\n')
            : '',
        ]
  return sections.filter(Boolean).join('\n\n')
}

function issueLine(i: main.LinearIssue): string {
  const head = `- ${i.id} ${i.title} [${i.state}]`
  if (DONE.includes(i.stateType) || !i.description) return head
  const d = i.description.length > DESCRIPTION_MAX ? i.description.slice(0, DESCRIPTION_MAX) + '…' : i.description
  return [head, ...d.split('\n').map((l) => '  ' + l)].join('\n')
}

// clashes finds ticked repos whose worktrees would land in the same folder (two
// repos sharing a worktree_root), keyed by repo name.
export function clashes(targets: Target[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const a of targets) {
    const b = targets.find((t) => t !== a && a.path && t.path === a.path)
    if (b) out[a.repo] = `same folder as ${b.repo}`
  }
  return out
}

// existingWorktree is a worktree already on branch; a detached one has no branch to match.
export function existingWorktree(branch: string, workspaces: main.WorkspaceInfo[]): main.WorkspaceInfo | undefined {
  return branch ? workspaces.find((w) => w.branch === branch) : undefined
}

// latest numbers requests so an answer to an older one can be dropped.
export function latest() {
  let n = 0
  return { next: () => ++n, current: (id: number) => id === n }
}
