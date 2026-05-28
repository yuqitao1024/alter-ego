/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: 'class',
  content: [
    './src/app/**/*.{js,ts,jsx,tsx,mdx}',
    './src/components/**/*.{js,ts,jsx,tsx,mdx}'
  ],
  theme: {
    extend: {
      colors: {
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        card: 'hsl(var(--card))',
        border: 'hsl(var(--border))',
        muted: 'hsl(var(--muted))',
        accent: 'hsl(var(--accent))',
        success: 'hsl(var(--success))',
        warning: 'hsl(var(--warning))',
        danger: 'hsl(var(--danger))'
      },
      boxShadow: {
        halo: '0 0 0 1px rgba(120, 255, 214, 0.12), 0 24px 80px rgba(2, 9, 19, 0.45)'
      },
      keyframes: {
        glow: {
          '0%, 100%': { opacity: '0.45' },
          '50%': { opacity: '1' }
        },
        rise: {
          '0%': { opacity: '0', transform: 'translateY(18px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        }
      },
      animation: {
        glow: 'glow 3s ease-in-out infinite',
        rise: 'rise 600ms cubic-bezier(0.16, 1, 0.3, 1) both'
      }
    }
  },
  plugins: []
}
