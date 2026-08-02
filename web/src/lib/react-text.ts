import { isValidElement } from 'react'
import type { ReactNode } from 'react'

function flatten(node: ReactNode): string {
  if (node === null || node === undefined || typeof node === 'boolean') {
    return ''
  }
  if (typeof node === 'string') {
    return node
  }
  if (typeof node === 'number') {
    return String(node)
  }
  if (Array.isArray(node)) {
    return node.map(flatten).join(' ')
  }
  if (isValidElement<{ children?: ReactNode }>(node)) {
    return flatten(node.props.children)
  }
  return ''
}

/**
 * Recursively concatenates the text content of a ReactNode tree (strings,
 * numbers, and element children) so JSON-LD can reuse exactly what renders
 * on the page instead of a hand-duplicated copy that can drift from it.
 */
export function reactNodeToText(node: ReactNode): string {
  return flatten(node).replace(/\s+/g, ' ').trim()
}
