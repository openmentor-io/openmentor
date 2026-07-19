import type { GetServerSideProps } from 'next'
import { withSSRObservability } from '@/lib/with-ssr-observability'

// Stable community-Slack entry point: 302-redirects to the workspace's
// current shareable invite link (SLACK_INVITE_URL, read at request time so
// a link rotation only needs an env update + container restart). Slack
// invite links expire (30 days max on the free plan), which is why every
// email and page links HERE instead of embedding the raw link. 404s when
// no link is configured.
const _getServerSideProps: GetServerSideProps = async () => {
  const inviteUrl = process.env.SLACK_INVITE_URL?.trim()

  if (!inviteUrl) {
    return { notFound: true }
  }

  return {
    redirect: {
      destination: inviteUrl,
      permanent: false, // the target rotates; never let clients cache it
    },
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'slack')

// Never rendered: getServerSideProps always redirects or 404s.
export default function Slack(): null {
  return null
}
