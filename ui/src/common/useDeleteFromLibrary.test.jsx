import { renderHook, act } from '@testing-library/react-hooks'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useDeleteFromLibrary } from './useDeleteFromLibrary'

const {
  mockConfig,
  mockPermissions,
  mockDeleteFromLibrary,
  mockNotify,
  mockRefresh,
} = vi.hoisted(() => ({
  mockConfig: { enableDeletion: true },
  mockPermissions: { value: 'admin' },
  mockDeleteFromLibrary: vi.fn(),
  mockNotify: vi.fn(),
  mockRefresh: vi.fn(),
}))

vi.mock('../config', () => ({ default: mockConfig }))

vi.mock('react-admin', () => ({
  useDataProvider: () => ({ deleteFromLibrary: mockDeleteFromLibrary }),
  useNotify: () => mockNotify,
  usePermissions: () => ({ permissions: mockPermissions.value }),
  useRefresh: () => mockRefresh,
}))

describe('useDeleteFromLibrary', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockConfig.enableDeletion = true
    mockPermissions.value = 'admin'
    mockDeleteFromLibrary.mockResolvedValue({
      data: { count: 1, ids: ['mf1'] },
    })
  })

  describe('availability', () => {
    it('is enabled for an admin when the server allows deletion', () => {
      const { result } = renderHook(() => useDeleteFromLibrary('song'))
      expect(result.current.enabled).toBe(true)
    })

    it('is disabled for a regular user', () => {
      mockPermissions.value = 'regular'
      const { result } = renderHook(() => useDeleteFromLibrary('song'))
      expect(result.current.enabled).toBe(false)
    })

    it('is disabled when the server has the feature off', () => {
      mockConfig.enableDeletion = false
      const { result } = renderHook(() => useDeleteFromLibrary('song'))
      expect(result.current.enabled).toBe(false)
    })
  })

  describe('confirmation', () => {
    it('does not call the endpoint until confirmed', async () => {
      const { result } = renderHook(() => useDeleteFromLibrary('song'))

      act(() => result.current.requestDelete(['mf1']))
      expect(result.current.isOpen).toBe(true)
      expect(mockDeleteFromLibrary).not.toHaveBeenCalled()

      await act(async () => {
        await result.current.confirm()
      })
      expect(mockDeleteFromLibrary).toHaveBeenCalledWith('song', ['mf1'])
    })

    it('does nothing when cancelled', async () => {
      const { result } = renderHook(() => useDeleteFromLibrary('song'))

      act(() => result.current.requestDelete(['mf1']))
      act(() => result.current.cancel())

      expect(result.current.isOpen).toBe(false)
      await act(async () => {
        await result.current.confirm()
      })
      expect(mockDeleteFromLibrary).not.toHaveBeenCalled()
    })

    it('ignores a request with no ids rather than opening an empty dialog', () => {
      const { result } = renderHook(() => useDeleteFromLibrary('song'))
      act(() => result.current.requestDelete([]))
      expect(result.current.isOpen).toBe(false)
    })

    it('drops falsy ids', () => {
      const { result } = renderHook(() => useDeleteFromLibrary('album'))
      act(() => result.current.requestDelete([undefined, null, '']))
      expect(result.current.isOpen).toBe(false)
    })
  })

  describe('after deleting', () => {
    it('notifies with the server-reported count and refreshes', async () => {
      mockDeleteFromLibrary.mockResolvedValue({ data: { count: 3 } })
      const { result } = renderHook(() => useDeleteFromLibrary('album'))

      act(() => result.current.requestDelete(['al1']))
      await act(async () => {
        await result.current.confirm()
      })

      expect(mockDeleteFromLibrary).toHaveBeenCalledWith('album', ['al1'])
      expect(mockNotify).toHaveBeenCalledWith(
        'message.deleteFromLibrarySuccess',
        {
          type: 'info',
          messageArgs: { smart_count: 3 },
        },
      )
      expect(mockRefresh).toHaveBeenCalled()
      expect(result.current.isOpen).toBe(false)
    })

    it('warns and does not refresh when the request fails', async () => {
      mockDeleteFromLibrary.mockRejectedValue(new Error('boom'))
      const { result } = renderHook(() => useDeleteFromLibrary('song'))

      act(() => result.current.requestDelete(['mf1']))
      await act(async () => {
        await result.current.confirm()
      })

      expect(mockNotify).toHaveBeenCalledWith(
        'message.deleteFromLibraryError',
        {
          type: 'warning',
          multiLine: true,
          duration: 0,
          messageArgs: { error: 'boom' },
        },
      )
      expect(mockRefresh).not.toHaveBeenCalled()
      expect(result.current.isOpen).toBe(false)
    })
  })
})
