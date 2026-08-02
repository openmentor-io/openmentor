import { useEffect } from 'react'
import { useRouter } from 'next/router'
import { NoIndexHead } from '@/components'

export default function AdminIndexPage(): JSX.Element {
  const router = useRouter()

  useEffect(() => {
    router.replace('/admin/mentors/pending')
  }, [router])

  return <NoIndexHead title="Moderation" />
}
