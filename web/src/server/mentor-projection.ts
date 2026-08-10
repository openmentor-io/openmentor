/**
 * Narrowing a catalog payload to the fields a page actually renders, before it
 * is handed to props.
 *
 * Everything returned from `getServerSideProps` is serialized into
 * `__NEXT_DATA__` and shipped to every visitor, and `drop_long_fields` only
 * clears `about`/`description` — a full `MentorListItem` still carries
 * `calendarUrl`, `isVisible`, `status`, `sortOrder`, `photo_url` and the rest,
 * which no catalog surface reads (C8).
 */
import type { MentorCardItem, MentorCatalogItem, MentorListItem } from '@/types'

/**
 * Narrow a mentor to the fields a card renders.
 *
 * `sessionsCount`, `photoStyle` and `updatedAt` are optional on the payload —
 * older Go API responses omit them — so they are spread in only when present.
 * Assigning them unconditionally would create own properties holding
 * `undefined`, which `getServerSideProps` rejects: `next dev` answers 500 with
 * "Error serializing `.mentors[0].sessionsCount`". Production hides it, since
 * JSON.stringify drops undefined, which makes it a bug that only breaks local
 * development — do not "simplify" this back into a plain destructure.
 */
export function toCardItem(mentor: MentorListItem): MentorCardItem {
  return {
    id: mentor.id,
    mentorId: mentor.mentorId,
    slug: mentor.slug,
    name: mentor.name,
    job: mentor.job,
    workplace: mentor.workplace,
    experience: mentor.experience,
    price: mentor.price,
    isNew: mentor.isNew,
    ...(mentor.sessionsCount !== undefined && { sessionsCount: mentor.sessionsCount }),
    ...(mentor.photoStyle !== undefined && { photoStyle: mentor.photoStyle }),
    ...(mentor.updatedAt !== undefined && { updatedAt: mentor.updatedAt }),
  }
}

/**
 * A card plus the three fields the homepage's client-side filters and search
 * work over: `tags` (topic pills), `menteeCount` ("no sessions yet") and
 * `competencies` (the search haystack, see `useMentors`).
 */
export function toCatalogItem(mentor: MentorListItem): MentorCatalogItem {
  return {
    ...toCardItem(mentor),
    tags: mentor.tags,
    menteeCount: mentor.menteeCount,
    competencies: mentor.competencies,
  }
}
