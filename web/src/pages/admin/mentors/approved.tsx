import Head from 'next/head'
import { AdminAuthProvider, MentorModerationListPage } from '@/components/admin-moderation'
import { pageTitle } from '@/config/seo'

export default function ApprovedMentorsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <Head>
        <title>{pageTitle('Approved mentors — moderation')}</title>
        <meta name="robots" content="noindex,follow" />
      </Head>
      <MentorModerationListPage status="approved" title="Approved Mentors" />
    </AdminAuthProvider>
  )
}
