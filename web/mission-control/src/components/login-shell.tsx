export function LoginShell() {
  return (
    <main className="min-h-screen px-0 py-0 xl:px-5 xl:py-5">
      <section className="relative w-full overflow-hidden bg-[rgba(7,16,28,0.72)] shadow-halo backdrop-blur-xl xl:min-h-[calc(100vh-2.5rem)] xl:rounded-[32px] xl:border xl:border-white/10">
        <div className="grid min-h-screen xl:min-h-[calc(100vh-2.5rem)] xl:grid-cols-[1.35fr_0.82fr]">
          <div className="relative overflow-hidden p-8 sm:p-10 lg:p-14 xl:p-16 2xl:p-20">
            <div className="absolute inset-0 bg-[radial-gradient(circle_at_20%_20%,rgba(76,222,196,0.22),transparent_0_32%),radial-gradient(circle_at_80%_12%,rgba(87,117,255,0.18),transparent_0_26%)]" />
            <div className="relative flex h-full flex-col justify-between">
              <div className="space-y-6">
                <div className="inline-flex items-center gap-3 rounded-full border border-white/10 bg-white/5 px-4 py-2 text-xs uppercase tracking-[0.28em] text-[rgba(180,228,222,0.78)]">
                  Secure Access
                </div>
                <div className="space-y-4">
                  <p className="text-sm uppercase tracking-[0.34em] text-[rgba(137,167,186,0.82)]">
                    Operator Login
                  </p>
                  <h1 className="max-w-4xl text-5xl font-semibold leading-[1.02] text-white lg:text-7xl xl:text-[5.6rem]">
                    Sign in to continue.
                  </h1>
                  <p className="max-w-3xl text-base leading-7 text-[rgba(184,200,214,0.82)] lg:text-lg">
                    Use Lark to open the dashboard and manage your tasks.
                  </p>
                </div>
              </div>

              <div className="grid gap-4 md:grid-cols-3">
                <div className="animate-rise rounded-3xl border border-white/10 bg-white/5 p-5 [animation-delay:80ms]">
                  <p className="text-xs uppercase tracking-[0.24em] text-[rgba(150,173,190,0.76)]">Identity</p>
                  <p className="mt-3 text-2xl font-semibold text-white">Lark</p>
                </div>
                <div className="animate-rise rounded-3xl border border-white/10 bg-white/5 p-5 [animation-delay:160ms]">
                  <p className="text-xs uppercase tracking-[0.24em] text-[rgba(150,173,190,0.76)]">Scope</p>
                  <p className="mt-3 text-2xl font-semibold text-white">Authorized</p>
                </div>
                <div className="animate-rise rounded-3xl border border-white/10 bg-white/5 p-5 [animation-delay:240ms]">
                  <p className="text-xs uppercase tracking-[0.24em] text-[rgba(150,173,190,0.76)]">Session</p>
                  <p className="mt-3 text-2xl font-semibold text-white">Protected</p>
                </div>
              </div>
            </div>
          </div>

          <div className="relative flex items-center border-t border-white/10 bg-[linear-gradient(180deg,rgba(255,255,255,0.02),rgba(255,255,255,0.01))] p-6 sm:p-8 lg:p-12 xl:border-l xl:border-t-0 xl:p-14 2xl:p-16">
            <div className="mx-auto w-full max-w-[680px] rounded-[28px] border border-white/10 bg-[rgba(6,11,19,0.82)] p-8 shadow-[0_24px_60px_rgba(0,0,0,0.35)] xl:p-10 2xl:p-12">
              <div className="mb-10 space-y-3">
                <p className="text-xs uppercase tracking-[0.3em] text-[rgba(145,184,176,0.78)]">Operator Identity</p>
                <h2 className="text-3xl font-semibold text-white">Sign in with Lark</h2>
                <p className="text-sm leading-6 text-[rgba(173,190,205,0.82)]">
                  Access is restricted to authorized operators.
                </p>
              </div>

              <a
                href="/auth/lark/start"
                className="group relative flex h-14 items-center justify-center overflow-hidden rounded-2xl border border-[rgba(94,238,205,0.3)] bg-[linear-gradient(120deg,rgba(56,205,174,0.18),rgba(79,95,242,0.22))] px-6 font-medium text-white transition duration-300 hover:scale-[1.01] hover:border-[rgba(132,255,224,0.48)]"
              >
                <span className="absolute inset-0 opacity-0 transition group-hover:opacity-100 bg-[radial-gradient(circle_at_center,rgba(148,255,231,0.26),transparent_60%)]" />
                <span className="relative">Continue with Lark</span>
              </a>

              <div className="mt-8 grid gap-3 text-sm text-[rgba(150,172,188,0.8)]">
                <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                  Unauthenticated sessions are redirected back to login.
                </div>
                <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                  Sign in to access the dashboard.
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>
  )
}
