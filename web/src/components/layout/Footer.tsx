import Link from 'next/link'
import Image from 'next/image'

/**
 * Redesign footer (design 01): navy band, white logomark + wordmark,
 * inline links, mono sign-off.
 */
export default function Footer(): JSX.Element {
  return (
    <footer className="bg-brand-navy" data-section="footer">
      <div className="flex flex-col gap-4 px-5 py-6 sm:flex-row sm:items-center sm:justify-between sm:px-8 sm:py-10 lg:px-16">
        <Link href="/" className="flex items-center gap-2.5">
          <Image
            src="/brand/logo/svg/logomark.svg"
            width={30}
            height={30}
            alt=""
            unoptimized
            className="brightness-0 invert"
          />
          <span className="font-display text-[15px] font-extrabold uppercase tracking-[-0.02em] text-white">
            openmentor.io
          </span>
        </Link>
        <nav className="flex flex-wrap gap-x-6 gap-y-2">
          <Link href="/donate" className="text-[13px] font-medium text-white/75 hover:text-white">
            Donate
          </Link>
          <Link href="/bementor" className="text-[13px] font-medium text-white/75 hover:text-white">
            Become a mentor
          </Link>
          <Link
            href="mailto:hello@openmentor.io"
            className="text-[13px] font-medium text-white/75 hover:text-white"
          >
            Email
          </Link>
          <Link href="/privacy" className="text-[13px] font-medium text-white/75 hover:text-white">
            Privacy
          </Link>
          <Link href="/terms" className="text-[13px] font-medium text-white/75 hover:text-white">
            Terms
          </Link>
        </nav>
        <span className="meta-mono text-white/50">Made with ♥ for the community</span>
      </div>
    </footer>
  )
}
