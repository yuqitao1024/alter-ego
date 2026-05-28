'use client'

import { useEffect, useState } from 'react'
import type { DashboardPayload, DashboardTask, WebSession } from '@/lib/types'

type DashboardShellProps = {
  initialSession: WebSession
}

const statusTone: Record<string, string> = {
  running: 'text-[rgb(108,240,188)] bg-[rgba(58,163,126,0.16)] border-[rgba(87,224,172,0.22)]',
  waiting_user_input: 'text-[rgb(255,209,122)] bg-[rgba(176,121,20,0.16)] border-[rgba(247,191,93,0.26)]',
  failed: 'text-[rgb(255,134,134)] bg-[rgba(184,60,60,0.14)] border-[rgba(255,122,122,0.22)]'
}

export function DashboardShell({ initialSession }: DashboardShellProps) {
  const [payload, setPayload] = useState<DashboardPayload | null>(null)
  const [selectedTask, setSelectedTask] = useState<DashboardTask | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    fetch('/api/web/dashboard', { credentials: 'include' })
      .then(async (res) => {
        if (!res.ok) {
          throw new Error('dashboard unavailable')
        }
        return res.json()
      })
      .then((data: DashboardPayload) => {
        if (!active) {
          return
        }
        setPayload(data)
        setSelectedTask(data.tasks[0] ?? null)
      })
      .catch(() => {
        if (!active) {
          return
        }
        setPayload({
          summary: {
            total: 0,
            pending: 0,
            starting: 0,
            running: 0,
            waiting_user_input: 0,
            recovering: 0,
            completed: 0,
            failed: 0,
            stopped: 0
          },
          tasks: []
        })
      })
      .finally(() => {
        if (active) {
          setLoading(false)
        }
      })
    return () => {
      active = false
    }
  }, [])

  const tasks = payload?.tasks ?? []

  return (
    <main className="min-h-screen px-5 py-5 lg:px-8 lg:py-7">
      <div className="mx-auto grid min-h-[calc(100vh-2.5rem)] max-w-[1540px] grid-cols-1 gap-5 lg:grid-cols-[110px_minmax(0,1fr)_360px]">
        <aside className="overflow-hidden rounded-[28px] border border-white/10 bg-[rgba(7,13,23,0.78)] p-5 shadow-halo backdrop-blur-xl">
          <div className="flex h-full flex-col justify-between">
            <div className="space-y-5">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl border border-white/10 bg-[linear-gradient(135deg,rgba(79,255,214,0.18),rgba(89,111,255,0.25))] text-lg font-semibold text-white">
                AE
              </div>
              <div className="space-y-3 text-[11px] uppercase tracking-[0.34em] text-[rgba(145,164,184,0.72)]">
                <div className="rounded-2xl border border-white/8 bg-white/[0.03] px-3 py-4 text-center text-[rgba(178,221,213,0.9)]">
                  Overview
                </div>
                <div className="rounded-2xl border border-transparent px-3 py-4 text-center">Tasks</div>
                <div className="rounded-2xl border border-transparent px-3 py-4 text-center">Signals</div>
              </div>
            </div>
            <form action="/auth/logout" method="post">
              <button className="w-full rounded-2xl border border-white/10 bg-white/[0.04] px-3 py-4 text-[11px] uppercase tracking-[0.28em] text-[rgba(180,198,212,0.78)] transition hover:border-white/20 hover:bg-white/[0.08]">
                Logout
              </button>
            </form>
          </div>
        </aside>

        <section className="overflow-hidden rounded-[30px] border border-white/10 bg-[rgba(8,15,26,0.78)] shadow-halo backdrop-blur-xl">
          <div className="border-b border-white/10 px-6 py-6 lg:px-8">
            <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
              <div className="space-y-3">
                <p className="text-xs uppercase tracking-[0.32em] text-[rgba(148,186,179,0.76)]">Alter Ego Mission Control</p>
                <h1 className="text-4xl font-semibold text-white lg:text-5xl">Task Overview Dashboard</h1>
                <p className="max-w-2xl text-sm leading-7 text-[rgba(177,193,208,0.78)]">
                  Browser cockpit backed by the live Alter Ego task store. Same-origin auth stays in Go; this shell reads real orchestration state.
                </p>
              </div>
              <div className="rounded-3xl border border-white/10 bg-white/[0.04] px-5 py-4 text-right">
                <p className="text-xs uppercase tracking-[0.28em] text-[rgba(141,160,177,0.76)]">Operator</p>
                <p className="mt-2 text-xl font-semibold text-white">{initialSession.name || initialSession.open_id}</p>
                <p className="mt-1 text-xs text-[rgba(140,171,162,0.76)]">{initialSession.open_id}</p>
              </div>
            </div>
          </div>

          <div className="grid gap-6 p-6 lg:grid-cols-[minmax(0,1fr)_minmax(360px,420px)] lg:p-8">
            <div className="space-y-6">
              <div className="grid gap-4 md:grid-cols-3">
                <MetricCard label="Running" value={loading ? '...' : String(payload?.summary.running ?? 0)} accent="rgba(75, 226, 177, 0.75)" />
                <MetricCard label="Waiting" value={loading ? '...' : String(payload?.summary.waiting_user_input ?? 0)} accent="rgba(255, 199, 97, 0.78)" />
                <MetricCard label="Completed" value={loading ? '...' : String(payload?.summary.completed ?? 0)} accent="rgba(133, 163, 255, 0.78)" />
              </div>

              <section className="rounded-[28px] border border-white/10 bg-[rgba(5,11,18,0.68)] p-5">
                <div className="mb-4 flex items-center justify-between">
                  <div>
                    <p className="text-xs uppercase tracking-[0.28em] text-[rgba(143,166,183,0.74)]">Live task feed</p>
                    <h2 className="mt-2 text-2xl font-semibold text-white">Live queue snapshot</h2>
                  </div>
                  <div className="rounded-full border border-[rgba(87,224,172,0.24)] bg-[rgba(63,208,170,0.1)] px-3 py-1 text-xs uppercase tracking-[0.22em] text-[rgba(147,241,213,0.84)]">
                    real data
                  </div>
                </div>

                <div className="overflow-hidden rounded-[22px] border border-white/8">
                  <div className="grid grid-cols-[1.2fr_0.8fr_2fr] bg-white/[0.03] px-4 py-3 text-[11px] uppercase tracking-[0.26em] text-[rgba(144,166,184,0.72)]">
                    <span>Task</span>
                    <span>Status</span>
                    <span>Summary</span>
                  </div>
                  <div className="divide-y divide-white/6">
                    {tasks.map((task, index) => (
                      <button
                        key={task.id}
                        className="grid w-full grid-cols-[1.2fr_0.8fr_2fr] gap-4 px-4 py-4 text-left transition hover:bg-white/[0.03]"
                        onClick={() => setSelectedTask(task)}
                        style={{ animationDelay: `${index * 70}ms` }}
                      >
                        <span>
                          <span className="block text-sm font-semibold text-white">{task.title}</span>
                          <span className="mt-1 block text-xs text-[rgba(143,165,184,0.72)]">{task.id} · {task.machine_id}</span>
                        </span>
                        <span>
                          <span className={`inline-flex rounded-full border px-3 py-1 text-xs uppercase tracking-[0.2em] ${statusTone[task.status] || 'text-[rgba(201,213,224,0.82)] border-white/10 bg-white/[0.04]'}`}>
                            {task.status}
                          </span>
                        </span>
                        <span className="text-sm leading-6 text-[rgba(178,194,207,0.8)]">{task.summary}</span>
                      </button>
                    ))}
                    {!loading && tasks.length === 0 ? (
                      <div className="px-4 py-8 text-sm text-[rgba(143,165,184,0.74)]">No tasks available.</div>
                    ) : null}
                  </div>
                </div>
              </section>
            </div>

            <aside className="rounded-[28px] border border-white/10 bg-[rgba(6,11,18,0.82)] p-6">
              <p className="text-xs uppercase tracking-[0.3em] text-[rgba(145,165,182,0.74)]">Detail panel</p>
              <h2 className="mt-3 text-2xl font-semibold text-white">
                {selectedTask?.title || 'Select a task'}
              </h2>
              <div className="mt-6 space-y-4">
                <DetailBlock label="Task ID" value={selectedTask?.id || 'No selection'} />
                <DetailBlock label="Status" value={selectedTask?.status || 'No selection'} />
                <DetailBlock label="Repository / Template" value={selectedTask ? `${selectedTask.repository_id} / ${selectedTask.template_id}` : 'No selection'} />
                <DetailBlock label="Latest summary" value={selectedTask?.summary || 'Choose a task row to inspect the live task payload.'} multiline />
                <DetailBlock label="Awaiting operator input" value={selectedTask?.awaiting_question?.question_text || 'No explicit operator question is pending.'} multiline />
                <DetailBlock
                  label="Recent signals"
                  value={
                    selectedTask?.recent_events?.length
                      ? selectedTask.recent_events.map((event) => `${event.event_type}: ${event.message}`).join('\n')
                      : 'No recent signals.'
                  }
                  multiline
                />
              </div>

              <div className="mt-8 rounded-[24px] border border-[rgba(92,112,255,0.18)] bg-[linear-gradient(180deg,rgba(70,94,245,0.12),rgba(14,22,41,0.02))] p-5">
                <p className="text-xs uppercase tracking-[0.24em] text-[rgba(162,176,255,0.76)]">Next slice</p>
                <p className="mt-3 text-sm leading-7 text-[rgba(185,197,223,0.8)]">
                  The next slice can add in-browser actions for stop, reply, reopen, and task completion without changing the data model again.
                </p>
              </div>
            </aside>
          </div>
        </section>
      </div>
    </main>
  )
}

function MetricCard({ label, value, accent }: { label: string; value: string; accent: string }) {
  return (
    <div className="animate-rise rounded-[26px] border border-white/10 bg-[rgba(255,255,255,0.04)] p-5 shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]">
      <p className="text-xs uppercase tracking-[0.3em] text-[rgba(145,166,182,0.72)]">{label}</p>
      <div className="mt-5 flex items-end justify-between">
        <p className="text-4xl font-semibold text-white">{value}</p>
        <span className="h-3 w-3 rounded-full animate-glow" style={{ backgroundColor: accent, boxShadow: `0 0 18px ${accent}` }} />
      </div>
    </div>
  )
}

function DetailBlock({ label, value, multiline }: { label: string; value: string; multiline?: boolean }) {
  return (
    <div className="rounded-[22px] border border-white/8 bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      <p className={`mt-3 text-sm text-[rgba(224,231,239,0.92)] ${multiline ? 'leading-7' : ''}`}>{value}</p>
    </div>
  )
}
