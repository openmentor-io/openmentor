import Link from 'next/link'
import { captureException } from '@/lib/posthog'
import type { NextPageContext } from 'next'

interface ErrorPageProps {
  statusCode: number
}

function ErrorPage({ statusCode }: ErrorPageProps): JSX.Element {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-white px-5 text-center">
      {/* Brand glyph: ring + cobalt stroke + mint node */}
      <div aria-hidden="true" className="relative h-[72px] w-[72px]">
        <div className="m-[7px] h-[58px] w-[58px] rounded-full border-8 border-brand-navy" />
        <div className="absolute -left-3 bottom-2.5 h-2 w-[34px] -rotate-[35deg] rounded-full bg-brand-cobalt" />
        <div className="absolute right-0.5 top-0.5 h-4 w-4 rounded-full bg-brand-mint" />
      </div>

      <p className="meta-mono mb-0 mt-6 text-ink-mute">Error {statusCode}</p>

      <h1 className="mt-2 text-3xl sm:text-[40px]">
        {statusCode === 404 ? 'Page not found' : 'Something broke'}
      </h1>

      <p className="mb-0 mt-3 max-w-[420px] text-[15px] leading-[1.6] text-ink-soft">
        {statusCode === 404
          ? 'Page not found.'
          : 'Something went wrong. We are already working on a fix.'}
      </p>

      <Link href="/" className="button-secondary mt-8">
        ← Back to all mentors
      </Link>
    </div>
  )
}

ErrorPage.getInitialProps = async ({ res, err }: NextPageContext): Promise<ErrorPageProps> => {
  const statusCode = res?.statusCode ?? err?.statusCode ?? 500
  if (err) {
    if (typeof window === 'undefined') {
      const { getPostHogServerClient } = await import('@/lib/posthog-server')
      const serverClient = getPostHogServerClient()
      serverClient?.captureException(err)
    } else {
      captureException(err)
    }
  }
  return { statusCode }
}

export default ErrorPage
