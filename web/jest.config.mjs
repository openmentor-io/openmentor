import nextJest from 'next/jest.js'

const createJestConfig = nextJest({ dir: './' })

const customJestConfig = {
  testEnvironment: 'jsdom',
  setupFilesAfterEnv: ['<rootDir>/jest.setup.ts'],
  moduleNameMapper: {
    '^@/(.*)$': '<rootDir>/src/$1',
  },
  // Raise the default 5s per-test timeout: the form tests drive many sequential
  // userEvent interactions that run comfortably under 5s locally but exceed it
  // on slower CI runners. 15s gives ample headroom without masking real hangs.
  testTimeout: 15000,
}

export default createJestConfig(customJestConfig)
