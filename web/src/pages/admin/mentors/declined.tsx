import { NoIndexHead } from '@/components'
import { AdminAuthProvider, MentorModerationListPage } from '@/components/admin-moderation'

export default function DeclinedMentorsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <NoIndexHead title="Declined mentors — moderation" />
      <MentorModerationListPage status="declined" title="Declined Mentors" />
    </AdminAuthProvider>
  )
}
