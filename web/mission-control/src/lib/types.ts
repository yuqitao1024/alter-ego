export type WebSession = {
  authenticated: boolean
  open_id: string
  name?: string
}

export type MockTask = {
  id: string
  title: string
  status: string
  summary: string
}

export type MockDashboardPayload = {
  summary: {
    running: number
    waiting: number
    failed: number
  }
  tasks: MockTask[]
}
