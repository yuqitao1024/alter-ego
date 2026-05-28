import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'Alter Ego Mission Control',
  description: 'Browser dashboard for Alter Ego phase 1'
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
