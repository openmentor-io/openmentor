import '@testing-library/jest-dom'

// Polyfill setImmediate for environments where it's missing (jsdom)
if (typeof global.setImmediate === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(global as any).setImmediate = (fn: (...args: unknown[]) => void, ...args: unknown[]) =>
    setTimeout(fn, 0, ...args)
}

// Polyfill ResizeObserver: jsdom doesn't implement it, and Headless UI v2 uses
// it for anchor positioning, so any test that opens a Menu/Popover throws
// "ResizeObserver is not defined". Observing nothing is fine here — the tests
// assert behaviour, not layout.
if (typeof global.ResizeObserver === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(global as any).ResizeObserver = class {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
}
