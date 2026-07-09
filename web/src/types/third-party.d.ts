declare module 'react-gtm-module' {
  interface TagManagerArgs {
    gtmId: string
    dataLayer?: Record<string, unknown>
    dataLayerName?: string
    events?: Record<string, unknown>
    auth?: string
    preview?: string
  }

  const TagManager: {
    initialize: (args: TagManagerArgs) => void
    dataLayer: (args: { dataLayer: Record<string, unknown> }) => void
  }

  export default TagManager
}
