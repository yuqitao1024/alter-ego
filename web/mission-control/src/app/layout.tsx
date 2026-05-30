import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'Alter Ego',
  description: 'Task workspace for your parallel intelligent self'
}

export default function RootLayout({
  children
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="en" className="dark">
      <body>{children}</body>
    </html>
  )
}
