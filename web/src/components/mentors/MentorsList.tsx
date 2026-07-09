import Image from 'next/image'
import Link from 'next/link'
import classNames from 'classnames'
import { imageLoader, updatedAtToVersion } from '@/lib/image-loader'
import { mentorPastelClass } from '@/lib/mentor-pastel'
import pluralize from '@/lib/pluralize'
import type { MentorListItem } from '@/types'

interface MentorsListProps {
  mentors: MentorListItem[]
  hasMore: boolean
  onClickMore: () => void
}

export default function MentorsList({
  mentors,
  hasMore,
  onClickMore,
}: MentorsListProps): JSX.Element {
  return (
    <>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 mb-8">
        {mentors.map((mentor) => {
          const firstName = mentor.name.split(' ')[0]

          // Prefer completed sessions when the (optional) field is present,
          // like the design's "4 sessions · $20"; fall back to experience.
          const metaLead =
            mentor.sessionsCount && mentor.sessionsCount > 0
              ? `${mentor.sessionsCount} ${pluralize(mentor.sessionsCount, 'session')}`
              : `${mentor.experience} years`

          return (
            <Link
              href={'/mentor/' + mentor.slug}
              target="_blank"
              key={mentor.id}
              className={classNames(
                'group flex min-h-[220px] flex-col rounded-2xl p-5 transition duration-200 hover:-translate-y-1 hover:shadow-lg sm:min-h-[280px]',
                mentorPastelClass(mentor.slug)
              )}
            >
              <div className="flex items-center gap-3">
                <div className="relative h-10 w-10 shrink-0 overflow-hidden rounded-full">
                  <Image
                    src={imageLoader({
                      src: mentor.slug,
                      quality: 'small',
                      version: updatedAtToVersion(mentor.updatedAt),
                    })}
                    alt={mentor.name}
                    fill
                    sizes="40px"
                    style={{ objectFit: 'cover' }}
                    unoptimized
                  />
                </div>

                <span className="truncate text-sm text-ink-mute">{firstName}</span>

                {mentor.isNew && (
                  <span className="ml-auto shrink-0 rounded-full bg-white/75 px-2.5 py-1 text-xs font-medium text-brand-navy">
                    New
                  </span>
                )}
              </div>

              <div className="mt-auto pt-8 sm:pt-14">
                <h3 className="text-xl font-semibold leading-snug line-clamp-3">{mentor.job}</h3>

                <div className="mt-2 text-sm text-ink-mute">
                  {metaLead} · {mentor.price}
                </div>
              </div>
            </Link>
          )
        })}
      </div>

      {hasMore && (
        <div className="text-center">
          <button className="button" onClick={() => onClickMore()}>
            Show more
          </button>
        </div>
      )}
    </>
  )
}
