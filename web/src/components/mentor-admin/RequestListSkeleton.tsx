/**
 * Loading shimmer rows for the requests inbox (design 07 / component sheet
 * loading state: 1.4s linear shimmer loop, paper tones only).
 */

interface RequestListSkeletonProps {
  rows?: number
}

export default function RequestListSkeleton({ rows = 4 }: RequestListSkeletonProps): JSX.Element {
  return (
    <div className="flex flex-col gap-2.5" aria-hidden="true" data-testid="request-list-skeleton">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-[18px] rounded-card border border-line bg-white px-[22px] py-[18px]"
        >
          <div className="h-11 w-11 flex-none animate-shimmer rounded-full bg-shimmer bg-[length:200%_100%]" />
          <div className="min-w-0 flex-1">
            <div className="h-4 w-2/5 animate-shimmer rounded-md bg-shimmer bg-[length:200%_100%]" />
            <div className="mt-2 h-3 w-4/5 animate-shimmer rounded-md bg-shimmer bg-[length:200%_100%]" />
          </div>
          <div className="h-6 w-20 flex-none animate-shimmer rounded-full bg-shimmer bg-[length:200%_100%]" />
        </div>
      ))}
    </div>
  )
}
