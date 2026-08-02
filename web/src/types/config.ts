/**
 * Configuration types
 */

/**
 * SEO configuration
 */
export interface SEOConfig {
  /** Homepage <title> and the default social title. */
  title: string
  /** Brand suffix for every other page's <title> — see pageTitle(). */
  titleSuffix: string
  description: string
  imageUrl: string
}

/**
 * Donate item configuration
 */
export interface DonateItem {
  name: string
  description: string
  linkUrl: string
}

/**
 * Logger configuration
 */
export interface LoggerConfig {
  MAX_FILE_SIZE: string
  MAX_FILES: string
  DATE_PATTERN: string
}

/**
 * HTTP client configuration
 */
export interface HttpConfig {
  TIMEOUT_MS: number
}

/**
 * Constants configuration
 */
export interface ConstantsConfig {
  METRICS_DEFAULT_LABELS: {
    service: string
    environment: string
    hostname: string
  }
}
