import { ApiError, getActiveRequests } from '@/lib/mentor-admin-api'

// Safari's JSON.parse failure, which is what response.json() threw at mentors
// when a revalidated response reached fetch() with no body.
const SAFARI_PARSE_ERROR = 'The string did not match the expected pattern.'

function mockJsonResponse(body: string): jest.Mock {
  const mock = jest.fn().mockResolvedValue({
    ok: true,
    status: 200,
    headers: { get: () => 'application/json; charset=utf-8' },
    text: async () => body,
    json: async () => {
      if (!body) throw new SyntaxError(SAFARI_PARSE_ERROR)
      return JSON.parse(body)
    },
  })
  global.fetch = mock as unknown as typeof fetch
  return mock
}

describe('mentor-admin-api apiRequest', () => {
  it('parses a JSON body', async () => {
    mockJsonResponse(JSON.stringify({ requests: [{ id: 'r1' }] }))

    await expect(getActiveRequests()).resolves.toEqual([{ id: 'r1' }])
  })

  it('turns an empty JSON body into a readable ApiError', async () => {
    mockJsonResponse('')

    const error = await getActiveRequests().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).message).not.toBe(SAFARI_PARSE_ERROR)
    expect((error as ApiError).message).toMatch(/reload the page/)
  })
})
