import Image from 'next/image'
import Link from 'next/link'
import classNames from 'classnames'
import { useEffect, useRef, useState } from 'react'
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
import { mentorInitialsClass, mentorPastelGradClass } from '@/lib/mentor-pastel'
import { isPriceFree, parsePriceAmount } from '@/config/filters'
import pluralize from '@/lib/pluralize'
import type { MentorListItem } from '@/types'

interface MentorsListProps {
  mentors: MentorListItem[]
  hasMore: boolean
  onClickMore: () => void
}

/** Cards that animate in on first mount (design motion spec: max 12). */
const ENTRANCE_CARD_COUNT = 12
/** Stagger between entrance cards. */
const ENTRANCE_STAGGER_MS = 40

/** '2-5' -> '2–5Y EXP', '10+' -> '10+Y EXP' (meta row, Plex Mono CAPS). */
export function formatExperience(experience: string): string {
  return `${experience.replace(/-/g, '–').toUpperCase()}Y EXP`
}

/** Two-letter initials for the no-photo fallback ("Ingrid Johansson" -> "IJ"). */
function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  const letters =
    parts.length >= 2 ? parts[0].charAt(0) + parts[1].charAt(0) : (parts[0] ?? '').slice(0, 2)
  return letters.toUpperCase()
}

/**
 * Free-text price -> card meta: FREE (mint ink) / $N (navy) / NEGOTIABLE
 * (stays ink-mute). Classification mirrors config/filters byPrice.
 */
function PriceMeta({ price }: { price: string }): JSX.Element {
  if (isPriceFree(price)) {
    return <span className="text-mint-ink">FREE</span>
  }

  const amount = parsePriceAmount(price)
  if (amount !== null) {
    return <span className="text-brand-navy">${amount.toLocaleString('en-US')}</span>
  }

  return <span>NEGOTIABLE</span>
}

function MentorCard({
  mentor,
  entranceIndex,
}: {
  mentor: MentorListItem
  /** 0-based stagger slot for the first-mount rise-in, or null for none. */
  entranceIndex: number | null
}): JSX.Element {
  // Photos are keyed by slug in object storage and the API payload carries
  // no explicit photo URL (uploading one is part of registration), so the
  // card always attempts the slug-based image and falls back to initials
  // when it doesn't exist (migrated/edge-case profiles).
  const [photoFailed, setPhotoFailed] = useState(false)
  const photoSrc = photoFailed
    ? null
    : imageLoader({
        src: mentor.slug,
        quality: 'large',
        version: updatedAtToVersion(mentor.updatedAt),
      })

  const sessions = mentor.sessionsCount ?? 0

  return (
    <Link
      href={'/mentor/' + mentor.slug}
      target="_blank"
      className={classNames(
        'group block overflow-hidden rounded-card border border-line bg-white transition-[transform,box-shadow] duration-180 hover:-translate-y-[3px] hover:shadow-card-hover',
        entranceIndex !== null && 'animate-rise-in'
      )}
      style={
        entranceIndex !== null
          ? { animationDelay: `${entranceIndex * ENTRANCE_STAGGER_MS}ms` }
          : undefined
      }
    >
      {/* Photo block: pastel gradient + one of three states (design 10 —
          hero cut-out / fallback A arch tile / fallback B initials). */}
      <div
        className={classNames(
          'relative h-[140px] sm:h-[200px]',
          mentorPastelGradClass(mentor.slug),
          photoSrc && mentor.photoStyle !== 'hero' && 'flex items-end justify-center',
          !photoSrc && 'flex items-center justify-center'
        )}
      >
        {photoSrc && mentor.photoStyle === 'hero' && (
          <Image
            src={photoSrc}
            alt=""
            width={170}
            height={180}
            unoptimized
            onError={() => setPhotoFailed(true)}
            className="absolute bottom-0 left-1/2 h-[122px] w-[112px] -translate-x-1/2 object-cover object-top contrast-[1.03] transition-transform duration-180 mix-blend-multiply group-hover:scale-[1.03] sm:h-[180px] sm:w-[170px]"
          />
        )}

        {photoSrc && mentor.photoStyle !== 'hero' && (
          <Image
            src={photoSrc}
            alt=""
            width={150}
            height={160}
            unoptimized
            onError={() => setPhotoFailed(true)}
            className="block h-[110px] w-[100px] rounded-t-panel border-[3px] border-b-0 border-white/75 object-cover object-[center_20%] transition-transform duration-180 group-hover:scale-[1.03] sm:h-[160px] sm:w-[150px]"
          />
        )}

        {!photoSrc && (
          <div
            className={classNames(
              'flex h-16 w-16 items-center justify-center rounded-full font-name text-[22px] font-bold text-white sm:h-[92px] sm:w-[92px] sm:text-[32px]',
              mentorInitialsClass(mentor.slug)
            )}
          >
            {initials(mentor.name)}
          </div>
        )}

        {/* Badge: NEW wins over the sessions count. */}
        {mentor.isNew ? (
          <span className="absolute left-2.5 top-2.5 rounded-md bg-brand-cobalt px-[9px] py-1 font-mono text-[10px] font-bold uppercase tracking-[0.06em] text-white sm:left-3 sm:top-3">
            NEW
          </span>
        ) : sessions > 0 ? (
          <span className="absolute left-2.5 top-2.5 rounded-md bg-white/90 px-2 py-1 font-mono text-[10px] font-medium uppercase tracking-[0.05em] text-brand-navy sm:left-3 sm:top-3">
            {sessions} {pluralize(sessions, 'SESSION', 'SESSIONS')}
          </span>
        ) : null}
      </div>

      <div className="border-t border-line px-3 pb-[13px] pt-[11px] sm:px-4 sm:pb-4 sm:pt-3.5">
        <div className="font-name text-[15px] font-bold leading-[1.15] tracking-[-0.015em] text-ink sm:text-[17px]">
          {mentor.name}
        </div>

        <div className="mt-[3px] text-xs leading-[1.4] text-ink-soft line-clamp-2 sm:text-[13px]">
          {mentor.job} · {mentor.workplace}
        </div>

        <div className="meta-mono mt-2 text-[10px] text-ink-mute sm:mt-2.5 sm:text-[11px]">
          {formatExperience(mentor.experience)} · <PriceMeta price={mentor.price} />
        </div>
      </div>
    </Link>
  )
}

export default function MentorsList({
  mentors,
  hasMore,
  onClickMore,
}: MentorsListProps): JSX.Element {
  // Entrance animation runs on first mount only — cards appearing later
  // (pagination, filter changes) render without it.
  const isFirstRender = useRef(true)
  useEffect(() => {
    isFirstRender.current = false
  }, [])

  return (
    <>
      <div className="mb-7 grid grid-cols-2 gap-3 lg:grid-cols-3 lg:gap-5 xl:grid-cols-4">
        {mentors.map((mentor, index) => (
          <MentorCard
            key={mentor.id}
            mentor={mentor}
            entranceIndex={isFirstRender.current && index < ENTRANCE_CARD_COUNT ? index : null}
          />
        ))}
      </div>

      {hasMore && (
        <div className="text-center">
          <button className="button-secondary px-[30px] py-[13px]" onClick={() => onClickMore()}>
            Show more mentors
          </button>
        </div>
      )}
    </>
  )
}
