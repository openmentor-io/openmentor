import { NoIndexHead } from '@/components'
import { AdminAuthProvider, MentorModerationListPage } from '@/components/admin-moderation'

export default function PendingMentorsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <NoIndexHead title="Pending mentors — moderation" />
      <MentorModerationListPage status="pending" title="Pending Mentors" />
    </AdminAuthProvider>
  )
}
