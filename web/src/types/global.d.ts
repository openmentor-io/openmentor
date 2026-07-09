import type { JSX as ReactJSX } from 'react'

declare global {
  namespace JSX {
    // Ensure JSX namespace is available under moduleResolution bundler
    export type Element = ReactJSX.Element
    export type IntrinsicElements = ReactJSX.IntrinsicElements
    export type IntrinsicAttributes = ReactJSX.IntrinsicAttributes
  }

  /**
   * Window interface augmentation for analytics
   */
  interface Window {
    dataLayer?: unknown[]
  }
}
