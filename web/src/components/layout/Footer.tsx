import Link from 'next/link'
import Image from 'next/image'

export default function Footer(): JSX.Element {
  return (
    <footer className="bg-primary-100" data-section="footer">
      <div className="py-8 text-center text-sm">
        <Link href="/" className="inline-block">
          <Image
            src="/brand/logo/svg/logo-horizontal.svg"
            width={117}
            height={32}
            alt="openmentor.io"
            unoptimized
          />
        </Link>
        <p>
          <Link className="link" href="mailto:hello@openmentor.io">
            Email
          </Link>
        </p>
        <p>
          <Link href="/privacy" className="link">
            Privacy Policy
          </Link>
        </p>
        <p>
          <Link href="/terms" className="link">
            Terms of Service
          </Link>
        </p>
      </div>
    </footer>
  )
}
