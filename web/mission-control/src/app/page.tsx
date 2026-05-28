import { redirect } from 'next/navigation'
import { headers } from 'next/headers'
import { DashboardShell } from '@/components/dashboard-shell'
import type { WebSession } from '@/lib/types'

async function fetchSession(): Promise<WebSession | null> {
  const headerStore = await headers()
  const response = await fetch(`${process.env.INTERNAL_API_BASE_URL || 'http://127.0.0.1:8080'}/api/web/session`, {
    headers: {
      Cookie: headerStore.get('cookie') || ''
    },
    cache: 'no-store'
  }).catch(() => null)

  if (!response || !response.ok) {
    return null
  }
  return response.json()
}

export default async function HomePage() {
  const session = await fetchSession()
  if (!session?.authenticated) {
    redirect('/login')
  }
  return <DashboardShell initialSession={session} />
}
