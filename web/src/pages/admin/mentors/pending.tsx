import Head from 'next/head'
import { AdminAuthProvider, MentorModerationListPage } from '@/components/admin-moderation'
import { pageTitle } from '@/config/seo'

export default function PendingMentorsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <Head>
        <title>{pageTitle('Pending mentors — moderation')}</title>
        <meta name="robots" content="noindex,follow" />
      </Head>
      <MentorModerationListPage status="pending" title="Pending Mentors" />
    </AdminAuthProvider>
  )
}
