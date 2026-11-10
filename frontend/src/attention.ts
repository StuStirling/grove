// What ⌘J goes to. Pure, so the order is unit-tested (attention.test.ts).

type Tab = { id: string; claude: string; since: number }

const rank: Record<string, number> = { waiting: 0, idle: 1 } // a permission prompt blocks; a finished turn can wait

// nextNeeding is the Claude tab that wants you most: waiting before finished,
// the longest-waiting first. The tab in front of you (current) is skipped, so
// pressing ⌘J again moves on.
export function nextNeeding(ws: { dir: string; panes?: Tab[] | null }[], current?: string | null): { dir: string; id: string } | undefined {
  const tabs = ws.flatMap((w) => (w.panes ?? []).filter((p) => p.claude in rank && p.id !== current).map((p) => ({ dir: w.dir, ...p })))
  tabs.sort((a, b) => rank[a.claude] - rank[b.claude] || a.since - b.since)
  return tabs[0] && { dir: tabs[0].dir, id: tabs[0].id }
}
