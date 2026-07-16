import Image from 'next/image'
import classNames from 'classnames'
import { useState } from 'react'
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
import { mentorInitialsClass, mentorPastelGradClass } from '@/lib/mentor-pastel'
import type { MentorBase } from '@/types'

/**
 * Mentor portrait block (redesign 02/03 + component sheet "full state set").
 *
 * Renders the mentor photo on the mentor's deterministic pastel gradient
 * with one of three treatments:
 * - 'hero'  — cut-out photo, bottom-anchored, multiply blend + contrast
 * - 'frame' — uncut photo, arch mask (rounded top) + white keyline
 * - initials fallback — navy/cobalt circle (by hash) when there is no photo
 */

/** First letters of the first two name words, e.g. "Ingrid Johansson" -> "IJ". */
export function mentorInitials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0].toUpperCase())
    .join('')
}

interface MentorPortraitProps {
  mentor: Pick<MentorBase, 'slug' | 'name' | 'photoStyle' | 'updatedAt'>
  /** Image quality passed to the storage loader. */
  quality?: 'small' | 'large' | 'full'
  /** `sizes` for next/image. */
  sizes?: string
  priority?: boolean
  /** Wrapper classes — must set a height (e.g. 'h-[310px]'). */
  className?: string
  /** Size of the photo box inside the pastel block ('hero' treatment). */
  heroBoxClassName?: string
  /** Size of the photo box inside the pastel block ('frame' treatment). */
  frameBoxClassName?: string
  /** Size/type classes for the initials fallback circle. */
  initialsClassName?: string
}

export default function MentorPortrait({
  mentor,
  quality = 'large',
  sizes = '320px',
  priority = false,
  className,
  heroBoxClassName = 'h-[90%] w-[81%] max-w-[260px]',
  frameBoxClassName = 'h-[80%] w-[60%] max-w-[200px]',
  initialsClassName = 'h-[84px] w-[84px] text-3xl',
}: MentorPortraitProps): JSX.Element {
  // Photos are keyed by slug in object storage; the payload carries no
  // explicit photo URL, so always attempt the image and fall back to the
  // initials circle when it doesn't exist.
  const [photoFailed, setPhotoFailed] = useState(false)
  const src = imageLoader({
    src: mentor.slug,
    quality,
    version: updatedAtToVersion(mentor.updatedAt),
  })

  return (
    <div
      className={classNames(
        'relative overflow-hidden',
        mentorPastelGradClass(mentor.slug),
        className
      )}
    >
      {photoFailed ? (
        <div className="flex h-full items-center justify-center">
          <div
            aria-hidden="true"
            className={classNames(
              'flex items-center justify-center rounded-full font-name font-bold text-white',
              mentorInitialsClass(mentor.slug),
              initialsClassName
            )}
          >
            {mentorInitials(mentor.name)}
          </div>
        </div>
      ) : mentor.photoStyle === 'hero' ? (
        <div
          className={classNames('absolute bottom-0 left-1/2 -translate-x-1/2', heroBoxClassName)}
        >
          <Image
            src={src}
            alt={mentor.name}
            fill
            sizes={sizes}
            priority={priority}
            unoptimized
            onError={() => setPhotoFailed(true)}
            className="mix-blend-multiply [filter:contrast(1.03)]"
            style={{ objectFit: 'cover', objectPosition: 'top' }}
          />
        </div>
      ) : (
        <div
          className={classNames(
            'absolute bottom-0 left-1/2 -translate-x-1/2 overflow-hidden rounded-t-panel border-[3px] border-b-0 border-white/75',
            frameBoxClassName
          )}
        >
          <Image
            src={src}
            alt={mentor.name}
            fill
            sizes={sizes}
            priority={priority}
            unoptimized
            onError={() => setPhotoFailed(true)}
            style={{ objectFit: 'cover', objectPosition: 'center 20%' }}
          />
        </div>
      )}
    </div>
  )
}
