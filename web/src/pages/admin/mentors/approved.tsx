import { NoIndexHead } from '@/components'
import { AdminAuthProvider, MentorModerationListPage } from '@/components/admin-moderation'

export default function ApprovedMentorsPage(): JSX.Element {
  return (
    <AdminAuthProvider>
      <NoIndexHead title="Approved mentors — moderation" />
      <MentorModerationListPage status="approved" title="Approved Mentors" />
    </AdminAuthProvider>
  )
}
