'use client'

import { useEffect, useRef, useState } from 'react'
import type { DashboardPayload, DashboardTask, DashboardTaskDetail, DashboardTaskEvent, DashboardTaskQuestion, TaskTemplate, WebSession } from '@/lib/types'

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
  const [templates, setTemplates] = useState<TaskTemplate[]>([])
  const [selectedTask, setSelectedTask] = useState<DashboardTask | null>(null)
  const [selectedTaskDetail, setSelectedTaskDetail] = useState<DashboardTaskDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailSection, setDetailSection] = useState<'overview' | 'timeline' | 'questions'>('overview')
  const [loading, setLoading] = useState(true)
  const [actionText, setActionText] = useState('')
  const [actionError, setActionError] = useState('')
  const [actionSuccess, setActionSuccess] = useState('')
  const [actionBusy, setActionBusy] = useState(false)
  const [busyTaskID, setBusyTaskID] = useState('')
  const [createTemplateID, setCreateTemplateID] = useState('')
  const [createRequirement, setCreateRequirement] = useState('')
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState('')
  const [createSuccess, setCreateSuccess] = useState('')
  const selectedTaskIDRef = useRef('')
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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

  async function loadTaskDetail(taskID: string) {
    const trimmed = taskID.trim()
    if (!trimmed) {
      setSelectedTaskDetail(null)
      return
    }

    setDetailLoading(true)
    return fetch(`/api/web/tasks/${trimmed}`, { credentials: 'include' })
      .then(async (res) => {
        if (!res.ok) {
          throw new Error('task detail unavailable')
        }
        return res.json()
      })
      .then((data: DashboardTaskDetail) => {
        setSelectedTaskDetail(data)
      })
      .catch(() => {
        setSelectedTaskDetail(null)
      })
      .finally(() => {
        setDetailLoading(false)
      })
  }

  async function loadTemplates() {
    return fetch('/api/web/templates', { credentials: 'include' })
      .then(async (res) => {
        if (!res.ok) {
          throw new Error('templates unavailable')
        }
        return res.json()
      })
      .then((data: TaskTemplate[]) => {
        setTemplates(data)
        setCreateTemplateID((current) => current || data[0]?.id || '')
      })
      .catch(() => {
        setTemplates([])
      })
  }

  useEffect(() => {
    let cancelled = false
    Promise.all([loadDashboard(), loadTemplates()]).finally(() => {
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
    setDetailSection('overview')
  }, [selectedTask?.id])

  useEffect(() => {
    selectedTaskIDRef.current = selectedTask?.id || ''
    if (!selectedTask?.id) {
      setSelectedTaskDetail(null)
      return
    }
    void loadTaskDetail(selectedTask.id)
  }, [selectedTask?.id])

  useEffect(() => {
    const source = new EventSource('/api/web/events')
    const refresh = () => {
      if (refreshTimerRef.current) {
        clearTimeout(refreshTimerRef.current)
      }
      refreshTimerRef.current = setTimeout(() => {
        void loadDashboard()
        if (selectedTaskIDRef.current) {
          void loadTaskDetail(selectedTaskIDRef.current)
        }
      }, 150)
    }

    source.addEventListener('task_updated', refresh)
    return () => {
      if (refreshTimerRef.current) {
        clearTimeout(refreshTimerRef.current)
      }
      source.removeEventListener('task_updated', refresh)
      source.close()
    }
  }, [])

  const tasks = payload?.tasks ?? []

  async function createTask() {
    if (createBusy) {
      return
    }
    if (!createTemplateID.trim() || !createRequirement.trim()) {
      setCreateError('Select a template and enter a requirement first.')
      return
    }

    setCreateBusy(true)
    setCreateError('')
    setCreateSuccess('')
    try {
      const response = await fetch('/api/web/tasks', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          template_id: createTemplateID.trim(),
          requirement: createRequirement.trim()
        })
      })
      if (!response.ok) {
        const message = await response.text()
        throw new Error(message || 'Task creation failed')
      }
      const created = await response.json()
      await loadDashboard()
      setCreateRequirement('')
      setCreateSuccess(`Started ${created.task_id || 'task'} successfully.`)
    } catch (error) {
      setCreateError(error instanceof Error ? error.message : 'Task creation failed')
    } finally {
      setCreateBusy(false)
    }
  }

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
      <div className="mx-auto grid min-h-[calc(100vh-2.5rem)] max-w-[1880px] grid-cols-1 gap-5 xl:grid-cols-[96px_minmax(0,1.24fr)_minmax(430px,0.86fr)]">
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
          <div className="border-b border-white/10 px-6 py-5 lg:px-8">
            <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
              <div className="space-y-2">
                <p className="text-xs uppercase tracking-[0.32em] text-[rgba(148,186,179,0.76)]">Alter Ego Mission Control</p>
                <h1 className="text-3xl font-semibold text-white lg:text-[2.8rem]">Task Overview Dashboard</h1>
                <p className="max-w-3xl text-sm leading-6 text-[rgba(177,193,208,0.78)]">
                  Browser cockpit backed by the live Alter Ego task store. Same-origin auth stays in Go; this shell reads real orchestration state.
                </p>
              </div>
              <div className="grid gap-3 sm:grid-cols-[minmax(260px,340px)]">
                <div className="rounded-3xl border border-white/10 bg-white/[0.04] px-5 py-4 text-right">
                  <p className="text-xs uppercase tracking-[0.28em] text-[rgba(141,160,177,0.76)]">Operator</p>
                  <p className="mt-2 text-xl font-semibold text-white">{initialSession.name || initialSession.open_id}</p>
                  <p className="mt-1 text-xs text-[rgba(140,171,162,0.76)]">{initialSession.open_id}</p>
                </div>
              </div>
            </div>
          </div>

          <div className="grid gap-6 p-6 2xl:grid-cols-[minmax(0,1.2fr)_minmax(470px,0.9fr)] lg:p-8">
            <div className="space-y-6">
              <TaskLaunchPanel
                templates={templates}
                createTemplateID={createTemplateID}
                createRequirement={createRequirement}
                createBusy={createBusy}
                createError={createError}
                createSuccess={createSuccess}
                onTemplateChange={setCreateTemplateID}
                onRequirementChange={setCreateRequirement}
                onCreate={createTask}
              />

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
                  <div className="hidden grid-cols-[1.15fr_0.72fr_1.9fr_0.95fr] bg-white/[0.03] px-4 py-3 text-[11px] uppercase tracking-[0.26em] text-[rgba(144,166,184,0.72)] xl:grid">
                    <span>Task</span>
                    <span>Status</span>
                    <span>Summary</span>
                    <span className="text-right">Actions</span>
                  </div>
                  <div className="divide-y divide-white/6">
                    {tasks.map((task, index) => (
                      <div
                        key={task.id}
                        className={`grid gap-4 px-4 py-4 transition hover:bg-white/[0.03] xl:grid-cols-[1.15fr_0.72fr_1.9fr_0.95fr] ${selectedTask?.id === task.id ? 'bg-white/[0.04]' : ''}`}
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
                        <div className="xl:flex xl:justify-end">
                          <InlineTaskActions
                            task={task}
                            busy={actionBusy && busyTaskID === task.id}
                            onSelect={() => setSelectedTask(task)}
                            onStop={() => runAction('stop', task)}
                            onComplete={() => runAction('complete', task)}
                            onDelete={() => runAction('delete', task)}
                          />
                        </div>
                      </div>
                    ))}
                    {!loading && tasks.length === 0 ? (
                      <div className="px-4 py-8 text-sm text-[rgba(143,165,184,0.74)]">No tasks available.</div>
                    ) : null}
                  </div>
                </div>
              </section>
            </div>

            <aside className="rounded-[28px] border border-white/10 bg-[rgba(6,11,18,0.82)] p-6 xl:sticky xl:top-5 xl:max-h-[calc(100vh-2.5rem)] xl:overflow-y-auto">
              <p className="text-xs uppercase tracking-[0.3em] text-[rgba(145,165,182,0.74)]">Detail panel</p>
              <h2 className="mt-3 text-2xl font-semibold text-white">
                {selectedTaskDetail?.title || selectedTask?.title || 'Select a task'}
              </h2>
              <div className="mt-6 flex flex-wrap gap-2">
                <DetailTabButton label="Overview" active={detailSection === 'overview'} onClick={() => setDetailSection('overview')} />
                <DetailTabButton label="Timeline" active={detailSection === 'timeline'} onClick={() => setDetailSection('timeline')} />
                <DetailTabButton label="Questions" active={detailSection === 'questions'} onClick={() => setDetailSection('questions')} />
              </div>
              <div className="mt-5 space-y-4">
                {detailSection === 'overview' ? (
                  <>
                    <DetailBlock label="Task ID" value={selectedTaskDetail?.id || selectedTask?.id || 'No selection'} />
                    <DetailBlock label="Status" value={selectedTaskDetail?.status || selectedTask?.status || 'No selection'} />
                    <DetailBlock
                      label="Repository / Template"
                      value={
                        selectedTaskDetail
                          ? `${selectedTaskDetail.repository_id} / ${selectedTaskDetail.template_id}`
                          : selectedTask
                            ? `${selectedTask.repository_id} / ${selectedTask.template_id}`
                            : 'No selection'
                      }
                    />
                    <DetailBlock
                      label="Latest summary"
                      value={selectedTaskDetail?.summary || selectedTask?.summary || 'Choose a task row to inspect the live task payload.'}
                      multiline
                    />
                    <DetailBlock
                      label="Awaiting operator input"
                      value={selectedTaskDetail?.awaiting_question?.question_text || selectedTask?.awaiting_question?.question_text || 'No explicit operator question is pending.'}
                      multiline
                    />
                  </>
                ) : null}
                {detailSection === 'timeline' ? (
                  <TimelineBlock
                    label="Event timeline"
                    loading={detailLoading}
                    events={selectedTaskDetail?.events || []}
                  />
                ) : null}
                {detailSection === 'questions' ? (
                  <QuestionHistoryBlock
                    label="Question history"
                    loading={detailLoading}
                    questions={selectedTaskDetail?.questions || []}
                  />
                ) : null}
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
                  Browser actions now reuse the same Go task service decisions, and detail inspection reads full task history from the store.
                </p>
              </div>
            </aside>
          </div>
        </section>
      </div>
    </main>
  )
}

function TaskLaunchPanel({
  templates,
  createTemplateID,
  createRequirement,
  createBusy,
  createError,
  createSuccess,
  onTemplateChange,
  onRequirementChange,
  onCreate
}: {
  templates: TaskTemplate[]
  createTemplateID: string
  createRequirement: string
  createBusy: boolean
  createError: string
  createSuccess: string
  onTemplateChange: (value: string) => void
  onRequirementChange: (value: string) => void
  onCreate: () => void
}) {
  return (
    <section className="rounded-[28px] border border-white/10 bg-[rgba(5,11,18,0.68)] p-5">
      <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-[rgba(143,166,183,0.74)]">Task Launch</p>
          <h2 className="mt-2 text-2xl font-semibold text-white">Start a new task</h2>
          <p className="mt-3 max-w-2xl text-sm leading-7 text-[rgba(177,193,208,0.78)]">
            Launch a new Codex task directly from the dashboard by selecting a template and writing the operator requirement.
          </p>
        </div>
        <button
          type="button"
          onClick={onCreate}
          disabled={createBusy}
          className="rounded-2xl border border-[rgba(87,224,172,0.24)] bg-[rgba(53,158,127,0.14)] px-5 py-3 text-sm font-medium text-[rgba(206,255,236,0.94)] transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {createBusy ? 'Starting task...' : 'Start task'}
        </button>
      </div>

      <div className="mt-5 grid gap-4 xl:grid-cols-[340px_minmax(0,1fr)]">
        <label className="rounded-[22px] border border-white/10 bg-white/[0.03] p-4">
          <span className="text-xs uppercase tracking-[0.22em] text-[rgba(144,165,183,0.72)]">Template</span>
          <select
            value={createTemplateID}
            onChange={(event) => onTemplateChange(event.target.value)}
            className="mt-3 w-full rounded-2xl border border-white/10 bg-[rgba(6,11,19,0.9)] px-4 py-3 text-sm text-white outline-none"
          >
            {templates.map((template) => (
              <option key={template.id} value={template.id}>
                {template.display_name || template.id}
              </option>
            ))}
          </select>
          <p className="mt-3 text-sm leading-6 text-[rgba(154,176,194,0.74)]">
            {templates.find((item) => item.id === createTemplateID)?.description || 'No template description available.'}
          </p>
        </label>

        <label className="rounded-[22px] border border-white/10 bg-white/[0.03] p-4">
          <span className="text-xs uppercase tracking-[0.22em] text-[rgba(144,165,183,0.72)]">Requirement</span>
          <textarea
            value={createRequirement}
            onChange={(event) => onRequirementChange(event.target.value)}
            placeholder="Describe what Codex should do next."
            className="mt-3 min-h-[126px] w-full rounded-[20px] border border-white/10 bg-[rgba(6,11,19,0.9)] px-4 py-3 text-sm text-white outline-none transition placeholder:text-[rgba(135,154,170,0.72)] focus:border-[rgba(91,208,180,0.42)]"
          />
        </label>
      </div>

      {createError ? (
        <p className="mt-4 rounded-2xl border border-[rgba(255,122,122,0.18)] bg-[rgba(160,45,45,0.1)] px-4 py-3 text-sm text-[rgba(255,185,185,0.92)]">
          {createError}
        </p>
      ) : null}

      {createSuccess ? (
        <p className="mt-4 rounded-2xl border border-[rgba(87,224,172,0.18)] bg-[rgba(53,158,127,0.12)] px-4 py-3 text-sm text-[rgba(206,255,236,0.94)]">
          {createSuccess}
        </p>
      ) : null}
    </section>
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

function DetailTabButton({
  label,
  active,
  onClick
}: {
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-full border px-4 py-2 text-[11px] uppercase tracking-[0.22em] transition ${
        active
          ? 'border-[rgba(87,224,172,0.24)] bg-[rgba(53,158,127,0.14)] text-[rgba(206,255,236,0.94)]'
          : 'border-white/10 bg-white/[0.03] text-[rgba(170,188,203,0.76)] hover:bg-white/[0.06]'
      }`}
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

function TimelineBlock({ label, loading, events }: { label: string; loading: boolean; events: DashboardTaskEvent[] }) {
  return (
    <div className="rounded-[22px] border border-white/8 bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      {loading ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">Loading detail timeline...</p> : null}
      {!loading && events.length === 0 ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">No recorded events.</p> : null}
      {!loading && events.length > 0 ? (
        <div className="mt-4 space-y-3">
          {events.map((event, index) => (
            <div key={`${event.event_type}-${event.created_at}-${index}`} className="rounded-2xl border border-white/8 bg-[rgba(4,10,17,0.48)] px-4 py-3">
              <div className="flex items-center justify-between gap-4">
                <p className="text-xs uppercase tracking-[0.2em] text-[rgba(145,196,182,0.84)]">{event.event_type}</p>
                <p className="text-[11px] text-[rgba(136,158,178,0.72)]">{formatTimestamp(event.created_at)}</p>
              </div>
              <p className="mt-2 text-sm leading-6 text-[rgba(224,231,239,0.92)]">{event.message}</p>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function QuestionHistoryBlock({ label, loading, questions }: { label: string; loading: boolean; questions: DashboardTaskQuestion[] }) {
  return (
    <div className="rounded-[22px] border border-white/8 bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      {loading ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">Loading question history...</p> : null}
      {!loading && questions.length === 0 ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">No recorded questions.</p> : null}
      {!loading && questions.length > 0 ? (
        <div className="mt-4 space-y-3">
          {questions.map((question, index) => (
            <div key={`${question.question_text}-${question.asked_at}-${index}`} className="rounded-2xl border border-white/8 bg-[rgba(4,10,17,0.48)] px-4 py-3">
              <div className="flex items-center justify-between gap-4">
                <p className="text-xs uppercase tracking-[0.2em] text-[rgba(255,214,145,0.88)]">{question.question_type || 'question'}</p>
                <p className="text-[11px] text-[rgba(136,158,178,0.72)]">{formatTimestamp(question.asked_at)}</p>
              </div>
              <p className="mt-2 text-sm leading-6 text-[rgba(224,231,239,0.92)]">{question.question_text}</p>
              {question.context_excerpt ? (
                <p className="mt-2 text-sm leading-6 text-[rgba(174,191,206,0.78)]">{question.context_excerpt}</p>
              ) : null}
              {question.options_summary ? (
                <p className="mt-2 text-sm leading-6 text-[rgba(154,176,194,0.78)]">Options: {question.options_summary}</p>
              ) : null}
              {question.answer_text ? (
                <p className="mt-3 rounded-xl border border-[rgba(87,224,172,0.14)] bg-[rgba(53,158,127,0.08)] px-3 py-2 text-sm leading-6 text-[rgba(208,248,232,0.92)]">
                  Answer: {question.answer_text}
                  {question.answered_at ? ` · ${formatTimestamp(question.answered_at)}` : ''}
                </p>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function formatTimestamp(value?: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}
