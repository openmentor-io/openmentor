import { useEffect } from 'react'
import Head from 'next/head'
import { useRouter } from 'next/router'
import { pageTitle } from '@/config/seo'

export default function AdminMentorsIndexPage(): JSX.Element {
  const router = useRouter()

  useEffect(() => {
    router.replace('/admin/mentors/pending')
  }, [router])

  return (
    <Head>
      <title>{pageTitle('Mentors')}</title>
      <meta name="robots" content="noindex,follow" />
    </Head>
  )
}
