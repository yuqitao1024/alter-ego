export function LoginShell() {
  return (
    <main className="flex min-h-screen items-center justify-center px-6 py-12">
      <section className="relative w-full max-w-[1560px] overflow-hidden rounded-[32px] border border-white/10 bg-[rgba(7,16,28,0.72)] shadow-halo backdrop-blur-xl">
        <div className="grid min-h-[720px] xl:grid-cols-[1.24fr_0.9fr]">
          <div className="relative overflow-hidden p-10 lg:p-14 xl:p-16">
            <div className="absolute inset-0 bg-[radial-gradient(circle_at_20%_20%,rgba(76,222,196,0.22),transparent_0_32%),radial-gradient(circle_at_80%_12%,rgba(87,117,255,0.18),transparent_0_26%)]" />
            <div className="relative flex h-full items-center">
              <div className="max-w-[760px] space-y-8">
                <div className="flex items-center gap-5">
                  <HeroBrandMark className="h-20 w-20 shrink-0 lg:h-24 lg:w-24" />
                  <div className="space-y-2">
                    <p className="text-xs uppercase tracking-[0.3em] text-[rgba(180,228,222,0.78)]">
                      Alter Ego
                    </p>
                    <p className="text-sm uppercase tracking-[0.3em] text-[rgba(137,167,186,0.82)]">
                      Another Intelligent Me
                    </p>
                  </div>
                </div>

                <div className="space-y-4">
                  <h1 className="max-w-3xl text-5xl font-semibold leading-[1.02] text-white lg:text-7xl xl:text-[5.35rem]">
                    Let another intelligent you keep work moving.
                  </h1>
                  <p className="max-w-2xl text-base leading-7 text-[rgba(184,200,214,0.82)] lg:text-lg">
                    One workspace to review active tasks, catch real blockers, and step in only when your decision matters.
                  </p>
                </div>

                <div className="grid gap-3 text-sm text-[rgba(179,198,212,0.8)] sm:grid-cols-3">
                  <div className="rounded-2xl bg-white/[0.04] px-4 py-4">
                    Supervisor
                  </div>
                  <div className="rounded-2xl bg-white/[0.04] px-4 py-4">
                    Task flow
                  </div>
                  <div className="rounded-2xl bg-white/[0.04] px-4 py-4">
                    Human judgment
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div className="relative flex items-center border-t border-white/10 bg-[linear-gradient(180deg,rgba(255,255,255,0.02),rgba(255,255,255,0.01))] p-8 lg:p-12 xl:border-l xl:border-t-0 xl:p-14">
            <div className="mx-auto w-full max-w-[560px] rounded-[28px] border border-white/10 bg-[rgba(6,11,19,0.82)] p-8 shadow-[0_24px_60px_rgba(0,0,0,0.35)] xl:p-10">
              <div className="mb-10 space-y-3">
                <p className="text-xs uppercase tracking-[0.3em] text-[rgba(145,184,176,0.78)]">Enter Workspace</p>
                <h2 className="text-3xl font-semibold text-white">Open your task workspace</h2>
                <p className="text-sm leading-6 text-[rgba(173,190,205,0.82)]">
                  Step into the console where your parallel self helps watch progress, surface blockers, and keep tasks moving.
                </p>
              </div>

              <a
                href="/auth/lark/start"
                className="group relative flex h-14 items-center justify-center overflow-hidden rounded-2xl border border-[rgba(94,238,205,0.3)] bg-[linear-gradient(120deg,rgba(56,205,174,0.18),rgba(79,95,242,0.22))] px-6 font-medium text-white transition duration-300 hover:scale-[1.01] hover:border-[rgba(132,255,224,0.48)]"
              >
                <span className="absolute inset-0 opacity-0 transition group-hover:opacity-100 bg-[radial-gradient(circle_at_center,rgba(148,255,231,0.26),transparent_60%)]" />
                <span className="relative">Enter with Lark</span>
              </a>

              <div className="mt-8 grid gap-3 text-sm text-[rgba(150,172,188,0.8)]">
                <div className="rounded-2xl bg-white/[0.03] px-4 py-4">
                  Review progress, inspect summaries, and answer only the decisions that truly need you.
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>
  )
}

function HeroBrandMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 96 96" className={className} aria-hidden="true">
      <defs>
        <linearGradient id="hero-shell" x1="12" y1="10" x2="84" y2="86" gradientUnits="userSpaceOnUse">
          <stop stopColor="#102036" />
          <stop offset="1" stopColor="#08111B" />
        </linearGradient>
        <linearGradient id="hero-left" x1="24" y1="28" x2="48" y2="74" gradientUnits="userSpaceOnUse">
          <stop stopColor="rgba(98,233,200,0.95)" />
          <stop offset="1" stopColor="rgba(98,233,200,0)" />
        </linearGradient>
        <linearGradient id="hero-right" x1="72" y1="28" x2="48" y2="74" gradientUnits="userSpaceOnUse">
          <stop stopColor="rgba(135,167,255,0.95)" />
          <stop offset="1" stopColor="rgba(135,167,255,0)" />
        </linearGradient>
        <linearGradient id="hero-face" x1="30" y1="22" x2="66" y2="60" gradientUnits="userSpaceOnUse">
          <stop stopColor="#EEF8FF" />
          <stop offset="1" stopColor="#C6D8EA" />
        </linearGradient>
        <linearGradient id="hero-spine" x1="48" y1="22" x2="48" y2="61" gradientUnits="userSpaceOnUse">
          <stop stopColor="#6EF0D0" />
          <stop offset="1" stopColor="#86A7FF" />
        </linearGradient>
        <linearGradient id="hero-visor" x1="35" y1="41" x2="61" y2="41" gradientUnits="userSpaceOnUse">
          <stop stopColor="#6EF0D0" />
          <stop offset="1" stopColor="#86A7FF" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="92" height="92" rx="28" fill="url(#hero-shell)" stroke="rgba(255,255,255,0.12)" />
      <path d="M31 27C25.8 30.8 22.5 36.7 22.5 43V52.5C22.5 64.4 32.1 74 44 74H48V62.5H45.3C36.6 62.5 29.5 55.4 29.5 46.7V44.1C29.5 38 32.6 32.4 37.8 29.1L31 27Z" fill="url(#hero-left)" />
      <path d="M65 27C70.2 30.8 73.5 36.7 73.5 43V52.5C73.5 64.4 63.9 74 52 74H48V62.5H50.7C59.4 62.5 66.5 55.4 66.5 46.7V44.1C66.5 38 63.4 32.4 58.2 29.1L65 27Z" fill="url(#hero-right)" />
      <path d="M48 21.5C38.5 21.5 30.8 29.2 30.8 38.7V46.2C30.8 55.7 38.5 63.4 48 63.4C57.5 63.4 65.2 55.7 65.2 46.2V38.7C65.2 29.2 57.5 21.5 48 21.5Z" fill="url(#hero-face)" />
      <path d="M48 23V62" stroke="url(#hero-spine)" strokeWidth="2.6" strokeLinecap="round" />
      <path d="M36.5 40.5C39.5 37.7 43.6 36.1 47.9 36.1C52.2 36.1 56.2 37.7 59.2 40.5" stroke="rgba(223,244,255,0.34)" strokeWidth="2.6" strokeLinecap="round" />
      <path d="M38.8 41C41.4 38.9 44.7 37.7 48.2 37.7H47.8C51.3 37.7 54.6 38.9 57.2 41" stroke="url(#hero-visor)" strokeWidth="8.2" strokeLinecap="round" />
      <circle cx="42.5" cy="41.5" r="1.7" fill="#EAF7FF" />
      <circle cx="53.5" cy="41.5" r="1.7" fill="#EAF7FF" />
      <path d="M43.6 51.2C45.2 53 47.5 54 50 54C52.5 54 54.8 53 56.4 51.2" stroke="rgba(234,247,255,0.85)" strokeWidth="2.8" strokeLinecap="round" />
    </svg>
  )
}
