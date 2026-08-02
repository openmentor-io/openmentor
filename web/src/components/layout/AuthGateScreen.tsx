import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircleNotch } from '@fortawesome/free-solid-svg-icons'
import NoIndexHead from './NoIndexHead'

/**
 * Full-screen spinner shown while a private page resolves auth or redirects.
 * It carries the page's noindex because this is the branch the server renders
 * and the one a crawler sees — auth state only resolves on the client.
 */
export default function AuthGateScreen({ title }: { title: string }): JSX.Element {
  return (
    <>
      <NoIndexHead title={title} />
      <div className="flex min-h-screen items-center justify-center bg-surface">
        <FontAwesomeIcon icon={faCircleNotch} className="animate-spin text-2xl text-brand-cobalt" />
      </div>
    </>
  )
}
