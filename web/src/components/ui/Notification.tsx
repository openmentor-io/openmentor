import { forwardRef, type ForwardedRef, type ReactNode } from 'react'

/**
 * Toast (component sheet §toasts): ink surface, 14px radius, white text,
 * mint check circle for success / danger "!" circle for error, rises in.
 */
interface NotificationProps {
  title?: ReactNode
  content: ReactNode
  /** Visual tone. Defaults to 'success' (mint check). */
  variant?: 'success' | 'error'
  onClose: () => void
}

function NotificationInner(
  { title, content, variant = 'success', onClose }: NotificationProps,
  ref: ForwardedRef<HTMLDivElement>
): JSX.Element {
  return (
    <div
      className="pointer-events-auto flex w-full max-w-sm animate-toast-in items-center gap-3 rounded-card bg-ink px-[18px] py-3.5 shadow-dropdown"
      ref={ref}
    >
      {variant === 'success' ? (
        <div
          className="flex h-[22px] w-[22px] flex-none items-center justify-center rounded-full bg-brand-mint"
          aria-hidden="true"
        >
          <svg width="10" height="8" viewBox="0 0 11 9" fill="none">
            <path d="M1 4.5L4 7.5L10 1" stroke="#161A20" strokeWidth="2" strokeLinecap="round" />
          </svg>
        </div>
      ) : (
        <div
          className="flex h-[22px] w-[22px] flex-none items-center justify-center rounded-full bg-danger text-[13px] font-bold text-white"
          aria-hidden="true"
        >
          !
        </div>
      )}

      <div className="min-w-0 flex-1">
        {!!title && <div className="text-[13px] font-semibold text-white">{title}</div>}

        <div className="text-[13px] font-medium text-white">{content}</div>
      </div>

      <button
        className="flex-none rounded-md text-white/60 transition-colors duration-120 hover:text-white"
        onClick={() => onClose()}
      >
        <span className="sr-only">Close</span>
        <svg
          xmlns="http://www.w3.org/2000/svg"
          className="h-4 w-4"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          aria-hidden={true}
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M6 18L18 6M6 6l12 12"
          />
        </svg>
      </button>
    </div>
  )
}

// This fixes bug when you try wrap Notification into @headless/ui Transition
// Function components not support refs, so everything breaks
const Notification = forwardRef<HTMLDivElement, NotificationProps>(NotificationInner)

export default Notification
