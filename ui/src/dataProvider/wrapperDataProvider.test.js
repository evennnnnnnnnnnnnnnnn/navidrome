import { describe, it, expect, vi, beforeEach } from 'vitest'
import wrapperDataProvider from './wrapperDataProvider'

const { mockProvider, mockHttpClient } = vi.hoisted(() => ({
  mockProvider: {
    update: vi.fn(),
    create: vi.fn(),
    getOne: vi.fn(),
  },
  mockHttpClient: vi.fn(),
}))

vi.mock('ra-data-json-server', () => ({ default: () => mockProvider }))
vi.mock('./httpClient', () => ({ default: mockHttpClient }))

describe('wrapperDataProvider', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mockProvider.update.mockResolvedValue({ data: { id: 'u1' } })
    mockProvider.create.mockResolvedValue({ data: { id: 'u1' } })
    mockHttpClient.mockResolvedValue({ json: [] })
  })

  describe('update user', () => {
    it('sets library associations when an admin edits a non-admin user', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: false, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalledWith(
        'user',
        expect.objectContaining({ id: 'u1' }),
      )
      expect(mockHttpClient).toHaveBeenCalledWith('/api/user/u1/library', {
        method: 'PUT',
        body: JSON.stringify({ libraryIds: [1] }),
      })
    })

    it('does not call the admin-only library endpoint when a non-admin edits their own profile', async () => {
      localStorage.setItem('role', 'regular')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: {
          name: 'Sam',
          isAdmin: false,
          libraryIds: [1],
          currentPassword: 'old',
          password: 'new',
        },
      })

      expect(mockProvider.update).toHaveBeenCalled()
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('does not set library associations when the edited user is an admin', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: true, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalled()
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('strips libraryIds from the user update payload', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: false, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalledWith(
        'user',
        expect.objectContaining({
          data: { name: 'Sam', isAdmin: false },
        }),
      )
    })
  })

  describe('refreshMetadata', () => {
    it('posts to the album metadata refresh endpoint', () => {
      mockHttpClient.mockResolvedValue({ json: {} })
      wrapperDataProvider.refreshMetadata('album', 'al-1')
      expect(mockHttpClient).toHaveBeenCalledWith(
        expect.stringContaining('/metadata/al/al-1/refresh'),
        { method: 'POST' },
      )
    })

    it('posts to the artist metadata refresh endpoint', () => {
      mockHttpClient.mockResolvedValue({ json: {} })
      wrapperDataProvider.refreshMetadata('artist', 'ar-1')
      expect(mockHttpClient).toHaveBeenCalledWith(
        expect.stringContaining('/metadata/ar/ar-1/refresh'),
        { method: 'POST' },
      )
    })

    // react-admin rejects a custom method whose response has no `data` key, and the
    // endpoint answers 204 with no body.
    it('resolves to a react-admin shaped response', async () => {
      mockHttpClient.mockResolvedValue({
        status: 204,
        body: '',
        json: undefined,
      })
      await expect(
        wrapperDataProvider.refreshMetadata('album', 'al-1'),
      ).resolves.toEqual({ data: { id: 'al-1' } })
    })
  })
  describe('deleteFromLibrary', () => {
    it('sends every id as a repeated query param', async () => {
      mockHttpClient.mockResolvedValue({
        json: { ids: ['mf1', 'mf2'], count: 2 },
      })

      await wrapperDataProvider.deleteFromLibrary('song', ['mf1', 'mf2'])

      expect(mockHttpClient).toHaveBeenCalledWith(
        '/api/deletion/song?id=mf1&id=mf2',
        { method: 'DELETE' },
      )
    })

    it('targets the album endpoint for albums', async () => {
      mockHttpClient.mockResolvedValue({ json: { ids: ['al1'], count: 3 } })

      const result = await wrapperDataProvider.deleteFromLibrary('album', [
        'al1',
      ])

      expect(mockHttpClient).toHaveBeenCalledWith(
        '/api/deletion/album?id=al1',
        {
          method: 'DELETE',
        },
      )
      expect(result).toEqual({ data: { ids: ['al1'], count: 3 } })
    })

    it('encodes ids that need it', async () => {
      mockHttpClient.mockResolvedValue({ json: {} })

      await wrapperDataProvider.deleteFromLibrary('song', ['a b&c'])

      expect(mockHttpClient).toHaveBeenCalledWith(
        '/api/deletion/song?id=a%20b%26c',
        { method: 'DELETE' },
      )
    })

    // The endpoint has no "delete all" mode; a bare request must not look like one.
    it('refuses an empty id list without calling the server', async () => {
      await expect(
        wrapperDataProvider.deleteFromLibrary('song', []),
      ).rejects.toThrow('No ids given')
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('refuses a missing id list without calling the server', async () => {
      await expect(
        wrapperDataProvider.deleteFromLibrary('song', undefined),
      ).rejects.toThrow('No ids given')
      expect(mockHttpClient).not.toHaveBeenCalled()
    })
  })
})
