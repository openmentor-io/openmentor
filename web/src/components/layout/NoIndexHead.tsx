import Head from 'next/head'
import { pageTitle } from '@/config/seo'

/**
 * Title + noindex for a private page (mentor portal, admin, transactional).
 *
 * Render it in EVERY return branch, the auth-loading and unauthenticated ones
 * included — those are what the server renders and what a crawler receives, so
 * a directive placed only in the authenticated branch never reaches the initial
 * response. robots.txt deliberately allows these URLs to be fetched precisely
 * so this tag can be seen (D35); a gated branch should use <AuthGateScreen>,
 * which carries it.
 */
export default function NoIndexHead({ title }: { title: string }): JSX.Element {
  return (
    <Head>
      <title>{pageTitle(title)}</title>
      <meta name="robots" content="noindex,follow" />
    </Head>
  )
}
