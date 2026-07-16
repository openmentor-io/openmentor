import Head from 'next/head'
import { useEffect, useState } from 'react'
import type { GetServerSideProps } from 'next'
import { Footer, MetaHeader, NavHeader, Section } from '@/components'
import RegisterMentorForm from '@/components/forms/RegisterMentorForm'
import analytics from '@/lib/analytics'
import { captureException } from '@/lib/posthog'
import seo from '@/config/seo'
import { withSSRObservability } from '@/lib/with-ssr-observability'
import logger, { getTraceContext } from '@/lib/logger'
import type { RegisterMentorRequest, RegisterMentorResponse } from '@/types/api'

// Add SSR observability for metrics, logs, and traces
const _getServerSideProps: GetServerSideProps = async (context) => {
  logger.info('Bementor page rendered', {
    userAgent: context.req.headers['user-agent'],
    ...getTraceContext(),
  })

  return {
    props: {},
  }
}

export const getServerSideProps = withSSRObservability(_getServerSideProps, 'register-mentor')

export default function Bementor(): JSX.Element {
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [submitStatus, setSubmitStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [errorMessage, setErrorMessage] = useState('')

  useEffect(() => {
    analytics.event(analytics.events.MENTOR_REGISTRATION_PAGE_VIEWED)
  }, [])

  const handleSubmit = async (data: RegisterMentorRequest): Promise<void> => {
    setIsSubmitting(true)
    setSubmitStatus('idle')
    setErrorMessage('')

    try {
      analytics.event(analytics.events.MENTOR_REGISTRATION_SUBMITTED, {
        outcome: 'submitted',
        experience: data.experience,
        price: data.price,
        tags_count: data.tags.length,
        has_calendar_url: Boolean(data.calendarUrl),
      })

      const response = await fetch('/api/register-mentor', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
      })

      const result: RegisterMentorResponse = await response.json()

      if (response.ok && result.success) {
        setSubmitStatus('success')
        analytics.event(analytics.events.MENTOR_REGISTRATION_SUBMITTED, {
          outcome: 'success',
          mentor_id: result.mentorId,
        })
      } else {
        setSubmitStatus('error')
        setErrorMessage(result.error || 'Something went wrong while submitting your application.')
        analytics.event(analytics.events.MENTOR_REGISTRATION_SUBMITTED, {
          outcome: 'error',
          error_type: 'api_error',
          status_code: response.status,
        })
      }
    } catch (error) {
      setSubmitStatus('error')
      setErrorMessage(
        'Something went wrong while submitting your application. Please try again later.'
      )
      if (error instanceof Error) {
        captureException(error, { page: 'bementor' })
      }
      analytics.event(analytics.events.MENTOR_REGISTRATION_SUBMITTED, {
        outcome: 'error',
        error_type: error instanceof Error ? error.name : 'unknown',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  const title = 'Join our team | ' + seo.title

  return (
    <>
      <Head>
        <title>{title}</title>
        <MetaHeader customTitle="Join our team" />
      </Head>

      <NavHeader />

      <Section className="bg-primary-100" id="header">
        <div className="text-center py-4 lg:w-3/4 mx-auto">
          <h1>Join our team</h1>

          <p>
            Helping others is honorable and cool. Thank you for wanting to do it.
            <br />
            Fill out the form below and we&apos;ll review your application as soon as we can.
          </p>
        </div>
      </Section>

      <Section>
        <div className="max-w-3xl mx-auto">
          {submitStatus === 'success' && (
            <div className="mb-8 p-6 bg-green-50 border border-green-200 rounded-2xl">
              <p className="font-bold text-green-800 mb-2">Thank you for your application!</p>
              <p className="text-green-700">
                Thanks! We&apos;ve received your application. A confirmation email is on its way. If
                it doesn&apos;t arrive, please check your spam folder and write to us at{' '}
                <a href="mailto:hello@openmentor.io">hello@openmentor.io</a>.
                <br />
                <br />
                IMPORTANT! Check your inbox and click the link in the confirmation email — your
                application goes to review only after you confirm your email address. Once
                confirmed, we try to review new applications as quickly as we can, but the process
                can take up to 2 weeks. If you don&apos;t hear from us,{' '}
                <a href="mailto:hello@openmentor.io">drop us a line</a> and we&apos;ll sort it out.
                <br />
                <br />
                Good luck!
              </p>
            </div>
          )}

          {submitStatus === 'error' && (
            <div className="mb-8 p-6 bg-red-50 border border-red-200 rounded-2xl">
              <p className="font-bold text-red-800 mb-2">Failed to submit your application</p>
              <p className="text-red-700">{errorMessage}</p>
            </div>
          )}

          {submitStatus !== 'success' && (
            <div className="rounded-2xl bg-surface p-6 sm:p-10">
              <RegisterMentorForm
                isLoading={isSubmitting}
                isError={submitStatus === 'error'}
                onSubmit={handleSubmit}
              />
            </div>
          )}
        </div>
      </Section>

      <Footer />
    </>
  )
}
