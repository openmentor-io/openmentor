import Head from 'next/head'
import Link from 'next/link'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import seo from '@/config/seo'

export default function NotFound(): JSX.Element {
  const title = '404 – Page not found | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="404 – Page not found" />
      </Head>

      <NavHeader />

      <Section className="bg-primary-100" id="header">
        <div className="text-center py-14 lg:w-1/2 mx-auto">
          <p className="text-8xl font-semibold text-primary mb-0">404</p>
          <h1 className="mt-4">Page not found</h1>
          <p className="pt-2">
            Looks like this page doesn&apos;t exist. It may have been moved or deleted.
          </p>
          <div className="mt-8">
            <Link href="/" className="button bg-primary-900 inline-block">
              Back to home
            </Link>
          </div>
        </div>
      </Section>

      <Footer />
    </>
  )
}
