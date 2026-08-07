import type {
  GetServerSideProps,
  GetServerSidePropsContext,
  GetServerSidePropsResult,
} from 'next'
import { pageViews, serverSideRenderDuration, mentorProfileViews } from './metrics'
import { logError } from './logger'

type SSRStatus = 'success' | 'redirect' | 'not_found' | 'error'

interface SSRResult<P> {
  props?: P
  redirect?: {
    destination: string
    permanent?: boolean
    statusCode?: number
  }
  notFound?: boolean
}

/**
 * Wraps getServerSideProps with observability instrumentation.
 *
 * Only for a page that genuinely needs per-request data. The seven content
 * pages that used to wrap a getServerSideProps whose whole body was one log
 * line are statically optimized now (C9): SSR on every request bought a
 * `nextjs_page_views_total{page="about"}` counter that Faro, PostHog, GTM and
 * the proxy access log already record, and cost the pages any CDN caching.
 * There is deliberately no getStaticProps wrapper — a build-time counter for a
 * page rendered once is not a page-view metric.
 */
export function withSSRObservability<P extends { [key: string]: unknown }>(
  getServerSidePropsFunc: GetServerSideProps<P>,
  pageName: string
): GetServerSideProps<P> {
  return async (context: GetServerSidePropsContext): Promise<GetServerSidePropsResult<P>> => {
    const start = Date.now()
    let status: SSRStatus = 'success'

    // Track page view
    pageViews.inc({ page: pageName })

    try {
      // Call the original function
      const result = (await getServerSidePropsFunc(context)) as SSRResult<P>

      // Check if it's a redirect or notFound
      if (result.redirect) {
        status = 'redirect'
      } else if (result.notFound) {
        status = 'not_found'
      }

      // Track mentor profile views if this is a mentor page
      if (pageName === 'mentor-detail' && context.params?.slug && status === 'success') {
        mentorProfileViews.inc()
      }

      const duration = (Date.now() - start) / 1000
      serverSideRenderDuration.observe({ page: pageName, status }, duration)

      return result as GetServerSidePropsResult<P>
    } catch (error) {
      status = 'error'
      const duration = (Date.now() - start) / 1000
      serverSideRenderDuration.observe({ page: pageName, status }, duration)

      if (error instanceof Error) {
        logError(error, {
          page: pageName,
          url: context.resolvedUrl,
          params: context.params,
        })
      }

      throw error
    }
  }
}
