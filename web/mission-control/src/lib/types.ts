export type WebSession = {
  authenticated: boolean
  open_id: string
  name?: string
}

export type TaskTemplate = {
  id: string
  display_name: string
  description: string
  repository_id: string
}

export type DashboardTaskEvent = {
  event_type: string
  message: string
  created_at: string
}

export type DashboardQuestion = {
  question_type: string
  question_text: string
  options_summary: string
  context_excerpt: string
  asked_at: string
}

export type DashboardTask = {
  id: string
  title: string
  created_by: string
  status: string
  summary: string
  template_id: string
  repository_id: string
  machine_id: string
  thread_id: string
  last_input: string
  last_updated_at: string
  created_at: string
  awaiting_question?: DashboardQuestion
  recent_events: DashboardTaskEvent[]
}

export type DashboardTaskQuestion = {
  question_type: string
  question_text: string
  options_summary: string
  context_excerpt: string
  asked_at: string
  answered_at?: string
  answer_text?: string
}

export type DashboardTaskDetail = {
  id: string
  title: string
  created_by: string
  status: string
  summary: string
  template_id: string
  repository_id: string
  machine_id: string
  thread_id: string
  last_input: string
  last_updated_at: string
  created_at: string
  awaiting_question?: DashboardQuestion
  events: DashboardTaskEvent[]
  questions: DashboardTaskQuestion[]
}

export type DashboardPayload = {
  summary: {
    total: number
    pending: number
    starting: number
    running: number
    waiting_user_input: number
    recovering: number
    completed: number
    failed: number
    stopped: number
  }
  tasks: DashboardTask[]
}
