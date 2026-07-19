import type { GetServerSidePropsContext } from 'next'
import { getServerSideProps } from '@/pages/slack'

// The /slack page is a stable redirect to the rotating Slack invite link
// (SLACK_INVITE_URL): emails link here so they survive link rotation.
describe('/slack redirect', () => {
  const context = {} as GetServerSidePropsContext
  const originalInviteUrl = process.env.SLACK_INVITE_URL

  afterEach(() => {
    process.env.SLACK_INVITE_URL = originalInviteUrl
  })

  it('302-redirects to the configured invite link', async () => {
    process.env.SLACK_INVITE_URL = 'https://join.slack.com/t/openmentor/shared_invite/abc123'

    const result = await getServerSideProps(context)

    expect(result).toEqual({
      redirect: {
        destination: 'https://join.slack.com/t/openmentor/shared_invite/abc123',
        permanent: false,
      },
    })
  })

  it('404s when no invite link is configured', async () => {
    delete process.env.SLACK_INVITE_URL

    const result = await getServerSideProps(context)

    expect(result).toEqual({ notFound: true })
  })

  it('404s when the configured link is blank', async () => {
    process.env.SLACK_INVITE_URL = '   '

    const result = await getServerSideProps(context)

    expect(result).toEqual({ notFound: true })
  })
})
