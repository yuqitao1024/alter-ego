'use client'

import { useEffect, useRef, useState } from 'react'
import type { DashboardPayload, DashboardTask, DashboardTaskDetail, DashboardTaskEvent, DashboardTaskQuestion, TaskTemplate, WebSession } from '@/lib/types'
import { MarkdownContent } from '@/components/markdown-content'

type DashboardShellProps = {
  initialSession: WebSession
}

const statusTone: Record<string, string> = {
  running: 'text-[rgb(108,240,188)] bg-[rgba(58,163,126,0.16)] border-[rgba(87,224,172,0.22)]',
  waiting_user_input: 'text-[rgb(255,209,122)] bg-[rgba(176,121,20,0.16)] border-[rgba(247,191,93,0.26)]',
  completed: 'text-[rgb(140,169,255)] bg-[rgba(67,90,199,0.14)] border-[rgba(133,163,255,0.2)]',
  stopped: 'text-[rgb(255,138,138)] bg-[rgba(166,54,54,0.12)] border-[rgba(255,138,138,0.18)]',
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
  const summary = payload?.summary
  const operatorName = initialSession.name?.trim() || 'Authorized user'
  const operatorID = initialSession.open_id?.trim() || ''

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
    <main className="min-h-screen px-3 py-3 xl:px-4 xl:py-4">
      <div className="grid min-h-[calc(100vh-1.5rem)] grid-cols-1 gap-4 xl:grid-cols-[108px_minmax(0,1fr)]">
        <aside className="hidden overflow-hidden rounded-[28px] border border-white/10 bg-[rgba(7,13,23,0.84)] px-2.5 py-5 shadow-halo backdrop-blur-xl xl:flex xl:flex-col xl:justify-between">
          <div>
            <BrandMark className="mx-auto h-14 w-14" />
            <div className="mt-6 rounded-2xl px-2 py-4 text-center text-[11px] uppercase tracking-[0.16em] text-[rgba(226,235,242,0.92)]">
              TASKS
            </div>
          </div>
          <form action="/auth/logout" method="post">
            <button className="w-full rounded-2xl px-2 py-4 text-[11px] uppercase tracking-[0.16em] text-[rgba(180,198,212,0.68)] transition hover:bg-white/[0.04] hover:text-[rgba(226,235,242,0.92)]">
              LOGOUT
            </button>
          </form>
        </aside>

        <section className="rounded-[30px] border border-white/10 bg-[rgba(8,15,26,0.78)] p-4 shadow-halo backdrop-blur-xl lg:p-5">
          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.618fr)_minmax(320px,1fr)]">
            <article className="rounded-[26px] bg-white/[0.04] px-6 py-6">
              <p className="text-xs uppercase tracking-[0.3em] text-[rgba(148,186,179,0.76)]">Task Console</p>
              <h1 className="mt-3 max-w-5xl text-4xl font-semibold leading-[0.96] text-white lg:text-5xl 2xl:text-[3.7rem]">
                Manage tasks, replies, and outcomes in one place.
              </h1>
              <p className="mt-4 max-w-4xl text-sm leading-7 text-[rgba(177,193,208,0.8)] lg:text-[15px]">
                Track active work, inspect the latest summaries, and reply only when Codex actually pauses for input.
              </p>
            </article>

            <aside className="rounded-[26px] bg-white/[0.04] px-5 py-5">
              <p className="text-xs uppercase tracking-[0.24em] text-[rgba(143,166,183,0.74)]">Task snapshot</p>
              <div className="mt-4 grid gap-3 sm:grid-cols-3 2xl:grid-cols-3">
                <SignalChip label="Running" value={loading ? '...' : String(summary?.running ?? 0)} />
                <SignalChip label="Waiting reply" value={loading ? '...' : String(summary?.waiting_user_input ?? 0)} />
                <SignalChip label="Completed" value={loading ? '...' : String(summary?.completed ?? 0)} />
              </div>
              <div className="mt-4 rounded-[20px] bg-[rgba(255,255,255,0.028)] px-4 py-4">
                <p className="text-xs uppercase tracking-[0.22em] text-[rgba(141,160,177,0.76)]">Signed in as</p>
                <p className="mt-2 text-lg font-semibold text-white">{operatorName}</p>
              </div>
            </aside>
          </div>

          <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(320px,420px)_minmax(0,1fr)]">
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

            <section className="grid gap-3 rounded-[26px] bg-white/[0.04] p-5 sm:grid-cols-2 2xl:grid-cols-4">
              <SignalChip label="Need reply" value={loading ? '...' : String(summary?.waiting_user_input ?? 0)} />
              <SignalChip label="Starting" value={loading ? '...' : String(summary?.starting ?? 0)} />
              <SignalChip label="Recovering" value={loading ? '...' : String(summary?.recovering ?? 0)} />
              <SignalChip label="Failed" value={loading ? '...' : String(summary?.failed ?? 0)} />
            </section>
          </div>

          <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.618fr)_minmax(320px,1fr)]">
            <section className="overflow-hidden rounded-[28px] border border-white/10 bg-[rgba(5,11,18,0.74)]">
              <div className="flex flex-col gap-4 border-b border-white/8 bg-white/[0.02] px-5 py-5 lg:flex-row lg:items-end lg:justify-between">
                <div>
                  <p className="text-xs uppercase tracking-[0.24em] text-[rgba(143,166,183,0.74)]">Task list</p>
                  <h2 className="mt-2 text-[1.75rem] font-semibold text-white">Current tasks</h2>
                </div>
                <div className="flex flex-wrap gap-2">
                  <button className="rounded-full border border-white/10 bg-white/[0.05] px-4 py-2 text-[11px] uppercase tracking-[0.18em] text-[rgba(215,226,235,0.86)]">All tasks</button>
                  <button className="rounded-full border border-white/10 bg-white/[0.03] px-4 py-2 text-[11px] uppercase tracking-[0.18em] text-[rgba(166,184,198,0.8)]">Waiting reply</button>
                  <button className="rounded-full border border-transparent bg-transparent px-4 py-2 text-[11px] uppercase tracking-[0.18em] text-[rgba(145,164,184,0.7)]">Recent first</button>
                </div>
              </div>

              <div className="px-3 pb-3">
                <div className="hidden grid-cols-[1.12fr_0.78fr_1.9fr_0.96fr] gap-4 px-4 py-4 text-[11px] uppercase tracking-[0.22em] text-[rgba(144,166,184,0.72)] lg:grid">
                  <span>Task</span>
                  <span>Status</span>
                  <span>Live summary</span>
                  <span className="text-right">Actions</span>
                </div>

                <div className="space-y-3">
                  {tasks.map((task, index) => (
                    <div
                      key={task.id}
                      className={`grid gap-4 rounded-[22px] px-4 py-4 transition hover:-translate-y-[1px] hover:bg-white/[0.05] lg:grid-cols-[1.12fr_0.78fr_1.9fr_0.96fr] ${
                        selectedTask?.id === task.id
                          ? 'border border-[rgba(105,235,199,0.2)] bg-white/[0.06] shadow-[0_0_0_1px_rgba(105,235,199,0.08)]'
                          : 'bg-white/[0.025]'
                      }`}
                      style={{ animationDelay: `${index * 70}ms` }}
                    >
                      <button className="text-left" onClick={() => setSelectedTask(task)}>
                        <span className="block text-[15px] font-semibold text-white">{task.title}</span>
                        <span className="mt-1 block text-xs text-[rgba(143,165,184,0.72)]">{task.id} · {task.machine_id} · {task.template_id}</span>
                        <span className="mt-2 inline-flex rounded-full bg-[rgba(255,255,255,0.05)] px-2.5 py-1 text-[10px] uppercase tracking-[0.16em] text-[rgba(210,223,234,0.82)]">
                          {task.created_by === operatorID ? 'Your task' : `Created by ${task.created_by || 'unknown'}`}
                        </span>
                      </button>
                      <button className="text-left" onClick={() => setSelectedTask(task)}>
                        <span className={`inline-flex rounded-full border px-3 py-1 text-[11px] uppercase tracking-[0.18em] ${statusTone[task.status] || 'text-[rgba(201,213,224,0.82)] border-white/10 bg-white/[0.04]'}`}>
                          {task.status}
                        </span>
                      </button>
                      <button className="text-left text-sm leading-7 text-[rgba(178,194,207,0.82)]" onClick={() => setSelectedTask(task)}>
                        {task.summary ? (
                          <MarkdownContent content={task.summary} compact clampLines={3} />
                        ) : (
                          'No summary yet.'
                        )}
                      </button>
                      <div className="lg:flex lg:justify-end">
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
                    <div className="rounded-[22px] bg-white/[0.025] px-4 py-8 text-sm text-[rgba(143,165,184,0.74)]">
                      No tasks available.
                    </div>
                  ) : null}
                </div>
              </div>
            </section>

            <aside className="rounded-[28px] border border-white/10 bg-[rgba(7,13,23,0.88)] p-5 xl:sticky xl:top-4 xl:max-h-[calc(100vh-2rem)] xl:overflow-y-auto">
              <p className="text-xs uppercase tracking-[0.24em] text-[rgba(145,165,182,0.74)]">Task detail</p>
              <h2 className="mt-3 text-[1.9rem] font-semibold text-white">
                {selectedTaskDetail?.title || selectedTask?.title || 'Select a task'}
              </h2>
              <div className="mt-5 flex flex-wrap gap-2">
                <DetailTabButton label="Overview" active={detailSection === 'overview'} onClick={() => setDetailSection('overview')} />
                <DetailTabButton label="Timeline" active={detailSection === 'timeline'} onClick={() => setDetailSection('timeline')} />
                <DetailTabButton label="Reply" active={detailSection === 'questions'} onClick={() => setDetailSection('questions')} />
              </div>

              <div className="mt-5 space-y-4">
                {detailSection === 'overview' ? (
                  <>
                    <DetailMarkdownBlock label="Latest summary" value={selectedTaskDetail?.summary || selectedTask?.summary || 'Choose a task to inspect its latest state.'} />
                    <DetailMarkdownBlock label="Pending reply" value={selectedTaskDetail?.awaiting_question?.question_text || selectedTask?.awaiting_question?.question_text || 'No pending reply.'} />
                    <DetailBlock label="Task ID" value={selectedTaskDetail?.id || selectedTask?.id || 'No selection'} />
                    <DetailBlock
                      label="Ownership"
                      value={
                        selectedTaskDetail
                          ? selectedTaskDetail.created_by === operatorID
                            ? `Your task · ${selectedTaskDetail.created_by || 'unknown'}`
                            : `Other user's task · ${selectedTaskDetail.created_by || 'unknown'}`
                          : selectedTask
                            ? selectedTask.created_by === operatorID
                              ? `Your task · ${selectedTask.created_by || 'unknown'}`
                              : `Other user's task · ${selectedTask.created_by || 'unknown'}`
                            : 'No selection'
                      }
                    />
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
                  </>
                ) : null}
                {detailSection === 'timeline' ? (
                  <TimelineBlock label="Activity" loading={detailLoading} events={selectedTaskDetail?.events || []} />
                ) : null}
                {detailSection === 'questions' ? (
                  <QuestionHistoryBlock label="Reply history" loading={detailLoading} questions={selectedTaskDetail?.questions || []} />
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

              <div className="mt-6 rounded-[22px] bg-[rgba(83,107,214,0.08)] px-4 py-4 text-sm leading-7 text-[rgba(220,228,255,0.92)]">
                Reply, reopen, stop, or complete the selected task here.
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
    <section className="rounded-[26px] bg-white/[0.04] p-5">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p className="text-xs uppercase tracking-[0.24em] text-[rgba(143,166,183,0.74)]">New task</p>
            <h2 className="mt-2 text-2xl font-semibold text-white">Create a task</h2>
          </div>
          <button
            type="button"
            onClick={onCreate}
            disabled={createBusy}
            className="rounded-full border border-[rgba(87,224,172,0.24)] bg-[linear-gradient(135deg,rgba(72,214,182,0.18),rgba(88,104,244,0.18))] px-5 py-3 text-sm font-medium uppercase tracking-[0.18em] text-[rgba(216,255,244,0.96)] transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {createBusy ? 'Starting...' : 'Start task'}
          </button>
        </div>

        <label className="rounded-[20px] bg-white/[0.03] p-4">
          <span className="text-xs uppercase tracking-[0.22em] text-[rgba(144,165,183,0.72)]">Template</span>
          <select
            value={createTemplateID}
            onChange={(event) => onTemplateChange(event.target.value)}
            className="mt-3 w-full rounded-[18px] border border-white/10 bg-[rgba(6,11,19,0.9)] px-4 py-3 text-sm text-white outline-none"
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

        <label className="rounded-[20px] bg-white/[0.03] p-4">
          <span className="text-xs uppercase tracking-[0.22em] text-[rgba(144,165,183,0.72)]">Requirement</span>
          <textarea
            value={createRequirement}
            onChange={(event) => onRequirementChange(event.target.value)}
            placeholder="Describe what Codex should do next."
            className="mt-3 min-h-[112px] w-full rounded-[18px] border border-white/10 bg-[rgba(6,11,19,0.9)] px-4 py-3 text-sm text-white outline-none transition placeholder:text-[rgba(135,154,170,0.72)] focus:border-[rgba(91,208,180,0.42)]"
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
    <div className="mt-5 rounded-[24px] bg-[rgba(255,255,255,0.03)] p-5">
      <p className="text-xs uppercase tracking-[0.22em] text-[rgba(150,173,190,0.76)]">Task actions</p>
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
            className="min-h-[110px] w-full rounded-[18px] border border-white/10 bg-[rgba(6,11,19,0.9)] px-4 py-3 text-sm text-white outline-none transition placeholder:text-[rgba(135,154,170,0.72)] focus:border-[rgba(91,208,180,0.42)]"
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

function SignalChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[18px] bg-[rgba(255,255,255,0.028)] px-4 py-4">
      <p className="text-[11px] uppercase tracking-[0.2em] text-[rgba(145,166,182,0.72)]">{label}</p>
      <p className="mt-3 text-[1.7rem] font-semibold text-white">{value}</p>
    </div>
  )
}

function BrandMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
      <defs>
        <linearGradient id="brand-shell" x1="8" y1="6" x2="56" y2="58" gradientUnits="userSpaceOnUse">
          <stop stopColor="rgba(16,28,45,1)" />
          <stop offset="1" stopColor="rgba(8,16,25,1)" />
        </linearGradient>
        <linearGradient id="brand-left" x1="15" y1="20" x2="32" y2="50" gradientUnits="userSpaceOnUse">
          <stop stopColor="rgba(98,233,200,0.92)" />
          <stop offset="1" stopColor="rgba(98,233,200,0)" />
        </linearGradient>
        <linearGradient id="brand-right" x1="49" y1="20" x2="32" y2="50" gradientUnits="userSpaceOnUse">
          <stop stopColor="rgba(135,167,255,0.92)" />
          <stop offset="1" stopColor="rgba(135,167,255,0)" />
        </linearGradient>
        <linearGradient id="brand-face" x1="21" y1="14" x2="43" y2="41" gradientUnits="userSpaceOnUse">
          <stop stopColor="#E8F5FF" />
          <stop offset="1" stopColor="#C7D7E8" />
        </linearGradient>
        <linearGradient id="brand-spine" x1="32" y1="14" x2="32" y2="40" gradientUnits="userSpaceOnUse">
          <stop stopColor="#6EF0D0" />
          <stop offset="1" stopColor="#86A7FF" />
        </linearGradient>
        <linearGradient id="brand-visor" x1="24" y1="28" x2="40" y2="28" gradientUnits="userSpaceOnUse">
          <stop stopColor="#6EF0D0" />
          <stop offset="1" stopColor="#86A7FF" />
        </linearGradient>
      </defs>
      <rect x="1" y="1" width="62" height="62" rx="18" fill="url(#brand-shell)" stroke="rgba(255,255,255,0.1)" />
      <path d="M21 18C17.2 20.8 15 25.1 15 29.8V36.2C15 44.4 21.6 51 29.8 51H32V43H30.2C24.1 43 19 37.9 19 31.8V30.1C19 25.9 21.2 22 24.8 19.7L21 18Z" fill="url(#brand-left)" />
      <path d="M43 18C46.8 20.8 49 25.1 49 29.8V36.2C49 44.4 42.4 51 34.2 51H32V43H33.8C39.9 43 45 37.9 45 31.8V30.1C45 25.9 42.8 22 39.2 19.7L43 18Z" fill="url(#brand-right)" />
      <path d="M32 14C25.4 14 20 19.4 20 26V31.2C20 37.8 25.4 43.2 32 43.2C38.6 43.2 44 37.8 44 31.2V26C44 19.4 38.6 14 32 14Z" fill="url(#brand-face)" />
      <path d="M32 15V42" stroke="url(#brand-spine)" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M24 27.6C26.3 25.4 29.4 24.2 32.6 24.2C35.8 24.2 38.8 25.4 41 27.6" stroke="rgba(223,244,255,0.34)" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M25.8 28C27.7 26.5 30.1 25.6 32.7 25.6H33.3C35.9 25.6 38.3 26.5 40.2 28" stroke="url(#brand-visor)" strokeWidth="5.8" strokeLinecap="round" />
      <circle cx="28.3" cy="28.4" r="1.15" fill="#EAF7FF" />
      <circle cx="35.7" cy="28.4" r="1.15" fill="#EAF7FF" />
      <path d="M28.8 35C29.9 36.2 31.4 36.8 33 36.8C34.6 36.8 36.1 36.2 37.2 35" stroke="rgba(234,247,255,0.82)" strokeWidth="1.9" strokeLinecap="round" />
    </svg>
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
    <div className="rounded-[22px] bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      <p className={`mt-3 text-sm text-[rgba(224,231,239,0.92)] ${multiline ? 'leading-7' : ''}`}>{value}</p>
    </div>
  )
}

function DetailMarkdownBlock({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[22px] bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      <div className="mt-3 text-sm text-[rgba(224,231,239,0.92)]">
        <MarkdownContent content={value} />
      </div>
    </div>
  )
}

function TimelineBlock({ label, loading, events }: { label: string; loading: boolean; events: DashboardTaskEvent[] }) {
  return (
    <div className="rounded-[22px] bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      {loading ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">Loading activity...</p> : null}
      {!loading && events.length === 0 ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">No recorded events.</p> : null}
      {!loading && events.length > 0 ? (
        <div className="mt-4 space-y-3">
          {events.map((event, index) => (
            <div key={`${event.event_type}-${event.created_at}-${index}`} className="rounded-2xl bg-[rgba(4,10,17,0.48)] px-4 py-3">
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
    <div className="rounded-[22px] bg-white/[0.03] p-4">
      <p className="text-xs uppercase tracking-[0.24em] text-[rgba(144,165,183,0.72)]">{label}</p>
      {loading ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">Loading reply history...</p> : null}
      {!loading && questions.length === 0 ? <p className="mt-3 text-sm text-[rgba(156,177,194,0.74)]">No recorded replies.</p> : null}
      {!loading && questions.length > 0 ? (
        <div className="mt-4 space-y-3">
          {questions.map((question, index) => (
            <div key={`${question.question_text}-${question.asked_at}-${index}`} className="rounded-2xl bg-[rgba(4,10,17,0.48)] px-4 py-3">
              <div className="flex items-center justify-between gap-4">
                <p className="text-xs uppercase tracking-[0.2em] text-[rgba(255,214,145,0.88)]">{question.question_type || 'question'}</p>
                <p className="text-[11px] text-[rgba(136,158,178,0.72)]">{formatTimestamp(question.asked_at)}</p>
              </div>
              <div className="mt-2 text-sm text-[rgba(224,231,239,0.92)]">
                <MarkdownContent content={question.question_text} compact />
              </div>
              {question.context_excerpt ? (
                <div className="mt-2 text-sm text-[rgba(174,191,206,0.78)]">
                  <MarkdownContent content={question.context_excerpt} compact />
                </div>
              ) : null}
              {question.options_summary ? (
                <div className="mt-2 text-sm text-[rgba(154,176,194,0.78)]">
                  <MarkdownContent content={`Options\n\n${question.options_summary}`} compact />
                </div>
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
