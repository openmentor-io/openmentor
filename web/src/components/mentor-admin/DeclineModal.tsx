/**
 * Decline Modal component
 *
 * Confirm sheet for declining a request with a reason and an optional
 * comment (design 08 MOTION note), on the redesign panel/field system.
 */

import { useState, useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch, faTimes } from '@fortawesome/free-solid-svg-icons'
import type { DeclineReasonValue } from '@/types'
import { DECLINE_REASONS } from '@/types'

interface DeclineModalProps {
  isOpen: boolean
  onClose: () => void
  onConfirm: (reason: DeclineReasonValue, comment?: string) => Promise<void>
  menteName: string
}

export default function DeclineModal({
  isOpen,
  onClose,
  onConfirm,
  menteName,
}: DeclineModalProps): JSX.Element | null {
  const [reason, setReason] = useState<DeclineReasonValue | ''>('')
  const [comment, setComment] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const modalRef = useRef<HTMLDivElement>(null)
  const firstInputRef = useRef<HTMLSelectElement>(null)

  // Reset form when modal opens
  useEffect(() => {
    if (isOpen) {
      setReason('')
      setComment('')
      setError(null)
      // Focus first input when modal opens
      setTimeout(() => firstInputRef.current?.focus(), 100)
    }
  }, [isOpen])

  // Handle escape key
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent): void => {
      if (e.key === 'Escape' && isOpen && !isSubmitting) {
        onClose()
      }
    }

    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [isOpen, isSubmitting, onClose])

  // Handle click outside
  const handleBackdropClick = (e: React.MouseEvent): void => {
    if (e.target === e.currentTarget && !isSubmitting) {
      onClose()
    }
  }

  const handleSubmit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()

    if (!reason) {
      setError('Please select a decline reason')
      return
    }

    setIsSubmitting(true)
    setError(null)

    try {
      await onConfirm(reason, comment.trim() || undefined)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 overflow-y-auto"
      aria-labelledby="decline-modal-title"
      role="dialog"
      aria-modal="true"
    >
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-ink/40 transition-opacity"
        onClick={handleBackdropClick}
      />

      {/* Modal */}
      <div className="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
        <div
          ref={modalRef}
          className="relative w-full transform overflow-hidden rounded-panel bg-white text-left shadow-dropdown transition-all sm:my-8 sm:max-w-lg"
        >
          {/* Header */}
          <div className="flex items-center justify-between border-b border-line px-6 py-4">
            <h3
              id="decline-modal-title"
              className="font-display text-sm font-extrabold uppercase tracking-[0.03em] text-ink"
            >
              Decline request
            </h3>
            <button
              onClick={onClose}
              disabled={isSubmitting}
              className="text-ink-soft transition-colors duration-120 hover:text-ink disabled:opacity-50"
              aria-label="Close"
            >
              <FontAwesomeIcon icon={faTimes} />
            </button>
          </div>

          {/* Content */}
          <form onSubmit={handleSubmit}>
            <div className="space-y-4 px-6 py-4">
              <p className="my-0 text-sm text-ink-soft">
                You are about to decline the request from{' '}
                <span className="font-semibold text-ink">{menteName}</span>. Please select a
                reason.
              </p>

              {/* Reason select */}
              <div>
                <label
                  htmlFor="decline-reason"
                  className="mb-1.5 block text-[13px] font-semibold text-ink"
                >
                  Decline reason <span className="text-danger">*</span>
                </label>
                <select
                  id="decline-reason"
                  ref={firstInputRef}
                  value={reason}
                  onChange={(e) => setReason(e.target.value as DeclineReasonValue)}
                  disabled={isSubmitting}
                  className="field"
                >
                  <option value="">Select a reason</option>
                  {DECLINE_REASONS.map((r) => (
                    <option key={r.value} value={r.value}>
                      {r.label}
                    </option>
                  ))}
                </select>
              </div>

              {/* Comment textarea */}
              <div>
                <label
                  htmlFor="decline-comment"
                  className="mb-1.5 block text-[13px] font-semibold text-ink"
                >
                  Comment <span className="font-normal text-ink-soft">(optional)</span>
                </label>
                <textarea
                  id="decline-comment"
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  disabled={isSubmitting}
                  rows={3}
                  placeholder="Additional information for the mentee..."
                  className="field"
                />
              </div>

              {/* Error message */}
              {error && (
                <p className="my-0 animate-shake text-sm font-medium text-danger" role="alert">
                  {error}
                </p>
              )}
            </div>

            {/* Footer */}
            <div className="flex flex-col-reverse gap-3 border-t border-line bg-surface px-6 py-4 sm:flex-row sm:justify-end">
              <button
                type="button"
                onClick={onClose}
                disabled={isSubmitting}
                className="button-secondary w-full text-sm sm:w-auto"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting || !reason}
                className="button-destructive w-full text-sm disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
              >
                {isSubmitting ? (
                  <>
                    <FontAwesomeIcon icon={faCircleNotch} className="mr-2 animate-spin" />
                    Declining...
                  </>
                ) : (
                  'Decline request'
                )}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}
