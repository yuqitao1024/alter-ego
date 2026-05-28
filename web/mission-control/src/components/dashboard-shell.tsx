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
  const [actionText, setActionText] = useState('')
  const [actionError, setActionError] = useState('')
  const [actionSuccess, setActionSuccess] = useState('')
  const [actionBusy, setActionBusy] = useState(false)
  const [busyTaskID, setBusyTaskID] = useState('')

  async function loadDashboard() {
    let active = true
    return fetch('/api/web/dashboard', { credentials: 'include' })
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
        setSelectedTask((current) => {
          if (!current) {
            return data.tasks[0] ?? null
          }
          return data.tasks.find((task) => task.id === current.id) ?? data.tasks[0] ?? null
        })
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
  }

  useEffect(() => {
    let cancelled = false
    loadDashboard().finally(() => {
      if (cancelled) {
        return
      }
    })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    setActionText('')
    setActionError('')
    setActionSuccess('')
  }, [selectedTask?.id])

  const tasks = payload?.tasks ?? []

  async function runAction(action: 'stop' | 'complete' | 'delete' | 'reply' | 'reopen', taskOverride?: DashboardTask | null) {
    const task = taskOverride ?? selectedTask
    if (!task || actionBusy) {
      return
    }
    if ((action === 'reply' || action === 'reopen') && !actionText.trim()) {
      setActionError('Input text is required for this action.')
      return
    }

    setActionBusy(true)
    setBusyTaskID(task.id)
    setActionError('')
    setActionSuccess('')
    try {
      const response = await fetch(`/api/web/tasks/${task.id}/${action}`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json'
        },
        body: action === 'reply' || action === 'reopen' ? JSON.stringify({ text: actionText.trim() }) : undefined
      })
      if (!response.ok) {
        const message = await response.text()
        throw new Error(message || 'Action failed')
      }
      setActionText('')
      await loadDashboard()
      setActionSuccess(actionSuccessMessage(action, task.title))
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Action failed')
    } finally {
      setActionBusy(false)
      setBusyTaskID('')
    }
  }

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
                      <div
                        key={task.id}
                        className={`grid grid-cols-[1.15fr_0.8fr_1.7fr_0.95fr] gap-4 px-4 py-4 transition hover:bg-white/[0.03] ${selectedTask?.id === task.id ? 'bg-white/[0.04]' : ''}`}
                        style={{ animationDelay: `${index * 70}ms` }}
                      >
                        <button className="text-left" onClick={() => setSelectedTask(task)}>
                          <span className="block text-sm font-semibold text-white">{task.title}</span>
                          <span className="mt-1 block text-xs text-[rgba(143,165,184,0.72)]">{task.id} · {task.machine_id}</span>
                        </button>
                        <button className="text-left" onClick={() => setSelectedTask(task)}>
                          <span className={`inline-flex rounded-full border px-3 py-1 text-xs uppercase tracking-[0.2em] ${statusTone[task.status] || 'text-[rgba(201,213,224,0.82)] border-white/10 bg-white/[0.04]'}`}>
                            {task.status}
                          </span>
                        </button>
                        <button className="text-left text-sm leading-6 text-[rgba(178,194,207,0.8)]" onClick={() => setSelectedTask(task)}>
                          {task.summary}
                        </button>
                        <InlineTaskActions
                          task={task}
                          busy={actionBusy && busyTaskID === task.id}
                          onSelect={() => setSelectedTask(task)}
                          onStop={() => runAction('stop', task)}
                          onComplete={() => runAction('complete', task)}
                          onDelete={() => runAction('delete', task)}
                        />
                      </div>
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

              <TaskControls
                task={selectedTask}
                actionText={actionText}
                actionError={actionError}
                actionSuccess={actionSuccess}
                actionBusy={actionBusy}
                onActionTextChange={setActionText}
                onStop={() => runAction('stop')}
                onComplete={() => runAction('complete')}
                onDelete={() => runAction('delete')}
                onReply={() => runAction('reply')}
                onReopen={() => runAction('reopen')}
              />

              <div className="mt-8 rounded-[24px] border border-[rgba(92,112,255,0.18)] bg-[linear-gradient(180deg,rgba(70,94,245,0.12),rgba(14,22,41,0.02))] p-5">
                <p className="text-xs uppercase tracking-[0.24em] text-[rgba(162,176,255,0.76)]">Next slice</p>
                <p className="mt-3 text-sm leading-7 text-[rgba(185,197,223,0.8)]">
                  Browser actions now reuse the same Go task service decisions. A later slice can add richer histories and optimistic refresh.
                </p>
              </div>
            </aside>
          </div>
        </section>
      </div>
    </main>
  )
}

function TaskControls({
  task,
  actionText,
  actionError,
  actionSuccess,
  actionBusy,
  onActionTextChange,
  onStop,
  onComplete,
  onDelete,
  onReply,
  onReopen
}: {
  task: DashboardTask | null
  actionText: string
  actionError: string
  actionSuccess: string
  actionBusy: boolean
  onActionTextChange: (value: string) => void
  onStop: () => void
  onComplete: () => void
  onDelete: () => void
  onReply: () => void
  onReopen: () => void
}) {
  if (!task) {
    return null
  }

  const canStop = task.status === 'running' || task.status === 'waiting_user_input'
  const canReply = task.status === 'waiting_user_input'
  const canComplete = task.status === 'waiting_user_input'
  const canDelete = task.status === 'completed' || task.status === 'failed' || task.status === 'stopped'
  const canReopen = task.status === 'completed' || task.status === 'stopped'

  return (
    <div className="mt-8 rounded-[24px] border border-white/10 bg-[rgba(255,255,255,0.03)] p-5">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(150,173,190,0.76)]">Controls</p>
      <div className="mt-4 flex flex-wrap gap-3">
        {canStop ? <ActionButton label="Stop task" disabled={actionBusy} onClick={onStop} tone="danger" /> : null}
        {canComplete ? <ActionButton label="Mark complete" disabled={actionBusy} onClick={onComplete} tone="primary" /> : null}
        {canDelete ? <ActionButton label="Delete task" disabled={actionBusy} onClick={onDelete} tone="danger" /> : null}
      </div>

      {canReply || canReopen ? (
        <div className="mt-5 space-y-3">
          <textarea
            value={actionText}
            onChange={(event) => onActionTextChange(event.target.value)}
            placeholder={canReply ? 'Reply to Codex to continue this task.' : 'Describe the extra requirement for reopening this task.'}
            className="min-h-[110px] w-full rounded-[20px] border border-white/10 bg-[rgba(6,11,19,0.9)] px-4 py-3 text-sm text-white outline-none transition placeholder:text-[rgba(135,154,170,0.72)] focus:border-[rgba(91,208,180,0.42)]"
          />
          <div className="flex flex-wrap gap-3">
            {canReply ? <ActionButton label="Send reply" disabled={actionBusy} onClick={onReply} tone="primary" /> : null}
            {canReopen ? <ActionButton label="Reopen task" disabled={actionBusy} onClick={onReopen} tone="secondary" /> : null}
          </div>
        </div>
      ) : null}

      {actionError ? (
        <p className="mt-4 rounded-2xl border border-[rgba(255,122,122,0.18)] bg-[rgba(160,45,45,0.1)] px-4 py-3 text-sm text-[rgba(255,185,185,0.92)]">
          {actionError}
        </p>
      ) : null}

      {actionSuccess ? (
        <p className="mt-4 rounded-2xl border border-[rgba(87,224,172,0.18)] bg-[rgba(53,158,127,0.12)] px-4 py-3 text-sm text-[rgba(206,255,236,0.94)]">
          {actionSuccess}
        </p>
      ) : null}
    </div>
  )
}

function InlineTaskActions({
  task,
  busy,
  onSelect,
  onStop,
  onComplete,
  onDelete
}: {
  task: DashboardTask
  busy: boolean
  onSelect: () => void
  onStop: () => void
  onComplete: () => void
  onDelete: () => void
}) {
  const showStop = task.status === 'running' || task.status === 'waiting_user_input'
  const showComplete = task.status === 'waiting_user_input'
  const showDelete = task.status === 'completed' || task.status === 'failed' || task.status === 'stopped'

  return (
    <div className="flex flex-wrap items-start justify-end gap-2">
      <button
        type="button"
        onClick={onSelect}
        className="rounded-full border border-white/10 bg-white/[0.04] px-3 py-1 text-[11px] uppercase tracking-[0.18em] text-[rgba(201,213,224,0.82)] transition hover:bg-white/[0.08]"
      >
        Inspect
      </button>
      {showStop ? <MiniActionButton label={busy ? 'Stopping' : 'Stop'} onClick={onStop} disabled={busy} tone="danger" /> : null}
      {showComplete ? <MiniActionButton label={busy ? 'Completing' : 'Complete'} onClick={onComplete} disabled={busy} tone="primary" /> : null}
      {showDelete ? <MiniActionButton label={busy ? 'Deleting' : 'Delete'} onClick={onDelete} disabled={busy} tone="danger" /> : null}
    </div>
  )
}

function MiniActionButton({
  label,
  onClick,
  disabled,
  tone
}: {
  label: string
  onClick: () => void
  disabled: boolean
  tone: 'primary' | 'danger'
}) {
  const toneClass =
    tone === 'danger'
      ? 'border-[rgba(255,122,122,0.18)] bg-[rgba(166,54,54,0.1)] text-[rgba(255,205,205,0.9)]'
      : 'border-[rgba(87,224,172,0.18)] bg-[rgba(53,158,127,0.1)] text-[rgba(200,255,236,0.9)]'
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`rounded-full border px-3 py-1 text-[11px] uppercase tracking-[0.18em] transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50 ${toneClass}`}
    >
      {label}
    </button>
  )
}

function actionSuccessMessage(action: 'stop' | 'complete' | 'delete' | 'reply' | 'reopen', taskTitle: string) {
  switch (action) {
    case 'stop':
      return `Stopped ${taskTitle}.`
    case 'complete':
      return `Marked ${taskTitle} as complete.`
    case 'delete':
      return `Deleted ${taskTitle}.`
    case 'reply':
      return `Reply sent for ${taskTitle}.`
    case 'reopen':
      return `Reopened ${taskTitle}.`
  }
}

function ActionButton({
  label,
  disabled,
  onClick,
  tone
}: {
  label: string
  disabled: boolean
  onClick: () => void
  tone: 'primary' | 'secondary' | 'danger'
}) {
  const toneClass =
    tone === 'danger'
      ? 'border-[rgba(255,122,122,0.24)] bg-[rgba(166,54,54,0.14)] text-[rgba(255,205,205,0.94)]'
      : tone === 'secondary'
        ? 'border-[rgba(133,163,255,0.24)] bg-[rgba(67,90,199,0.14)] text-[rgba(208,218,255,0.94)]'
        : 'border-[rgba(87,224,172,0.24)] bg-[rgba(53,158,127,0.14)] text-[rgba(200,255,236,0.94)]'

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={`rounded-2xl border px-4 py-3 text-sm font-medium transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50 ${toneClass}`}
    >
      {label}
    </button>
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
