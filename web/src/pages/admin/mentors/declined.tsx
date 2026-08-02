import Head from 'next/head'
import { AdminAuthProvider, MentorModerationListPage } from '@/components/admin-moderation'
import { pageTitle } from '@/config/seo'

export default function DeclinedMentorsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <Head>
        <title>{pageTitle('Declined mentors — moderation')}</title>
        <meta name="robots" content="noindex,follow" />
      </Head>
      <MentorModerationListPage status="declined" title="Declined Mentors" />
    </AdminAuthProvider>
  )
}
