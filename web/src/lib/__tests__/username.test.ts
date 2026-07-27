import {
  slugifyUsername,
  isUsernameFormatValid,
  USERNAME_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
} from '@/lib/username'

describe('slugifyUsername', () => {
  it('lowercases and hyphenates spaces', () => {
    expect(slugifyUsername('Anna Smith')).toBe('anna-smith')
  })

  it('collapses runs of non-alphanumerics into a single hyphen', () => {
    expect(slugifyUsername('John   D. Doe!!')).toBe('john-d-doe')
  })

  it('strips leading and trailing hyphens', () => {
    expect(slugifyUsername('  --Hello--  ')).toBe('hello')
  })

  it('strips combining diacritics', () => {
    expect(slugifyUsername('José Núñez')).toBe('jose-nunez')
  })

  it('truncates to the max length without a trailing hyphen', () => {
    const long = 'a'.repeat(50) + ' ' + 'b'.repeat(50)
    const result = slugifyUsername(long)
    expect(result.length).toBeLessThanOrEqual(USERNAME_MAX_LENGTH)
    expect(result.endsWith('-')).toBe(false)
  })

  it('returns empty string for input with no alphanumerics', () => {
    expect(slugifyUsername('!!! ???')).toBe('')
  })
})

describe('isUsernameFormatValid', () => {
  it('accepts a valid hyphenated username', () => {
    expect(isUsernameFormatValid('anna-smith')).toBe(true)
    expect(isUsernameFormatValid('dev2')).toBe(true)
  })

  it('rejects usernames that are too short or too long', () => {
    expect(isUsernameFormatValid('ab')).toBe(false)
    expect(isUsernameFormatValid('a'.repeat(USERNAME_MIN_LENGTH))).toBe(true)
    expect(isUsernameFormatValid('a'.repeat(USERNAME_MAX_LENGTH))).toBe(true)
    expect(isUsernameFormatValid('a'.repeat(USERNAME_MAX_LENGTH + 1))).toBe(false)
  })

  it('rejects leading, trailing and doubled hyphens', () => {
    expect(isUsernameFormatValid('-abc')).toBe(false)
    expect(isUsernameFormatValid('abc-')).toBe(false)
    expect(isUsernameFormatValid('ab--cd')).toBe(false)
  })

  it('rejects uppercase and non-alphanumeric characters', () => {
    expect(isUsernameFormatValid('Anna')).toBe(false)
    expect(isUsernameFormatValid('anna smith')).toBe(false)
    expect(isUsernameFormatValid('anna_smith')).toBe(false)
  })
})
