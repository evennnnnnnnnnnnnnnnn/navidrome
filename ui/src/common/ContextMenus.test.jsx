import React from 'react'
import { render, fireEvent, screen, waitFor } from '@testing-library/react'
import { TestContext } from 'ra-test'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { AlbumContextMenu, ArtistContextMenu } from './ContextMenus'

const mockDispatch = vi.fn()
vi.mock('react-redux', () => ({ useDispatch: () => mockDispatch }))

const { mockConfig } = vi.hoisted(() => ({
  mockConfig: {
    enableSharing: true,
    enableDownloads: true,
    enableFavourites: false,
    enableDeletion: false,
  },
}))
vi.mock('../config', () => ({ default: mockConfig }))

const { mockPermissions, mockRefreshMetadata, mockDeleteFromLibrary } =
  vi.hoisted(() => ({
    mockPermissions: { value: 'admin' },
    mockRefreshMetadata: vi.fn(),
    mockDeleteFromLibrary: vi.fn(),
  }))

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useNotify: () => vi.fn(),
    usePermissions: () => ({ permissions: mockPermissions.value }),
    useDataProvider: () => ({
      getList: vi.fn(),
      refreshMetadata: mockRefreshMetadata,
      deleteFromLibrary: mockDeleteFromLibrary,
    }),
    useRefresh: () => vi.fn(),
    useTranslate: () => (x) => x,
  }
})

describe('ContextMenus', () => {
  const renderMenu = (Menu, record, props = {}) => {
    render(
      <TestContext>
        <ThemeProvider theme={createTheme()}>
          <Menu record={record} {...props} />
        </ThemeProvider>
      </TestContext>,
    )
    fireEvent.click(screen.getByLabelText('more'))
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockConfig.enableSharing = true
    mockConfig.enableDownloads = true
    mockConfig.enableDeletion = false
    mockPermissions.value = 'admin'
  })

  describe('ArtistContextMenu', () => {
    const withAlbumArtist = {
      id: 'ar1',
      name: 'Artist',
      stats: { albumartist: { songCount: 3, albumCount: 1, size: 1024 } },
    }

    it('shows the album-artist size on the download item', () => {
      renderMenu(ArtistContextMenu, withAlbumArtist)
      expect(screen.getByText('ra.action.download (1 KB)')).toBeInTheDocument()
    })

    it('hides download and share for artists with no album-artist content', () => {
      renderMenu(ArtistContextMenu, { id: 'ar1', name: 'Artist', stats: {} })
      expect(screen.queryByText(/ra\.action\.download/)).not.toBeInTheDocument()
      expect(screen.queryByText('ra.action.share')).not.toBeInTheDocument()
    })
  })

  describe('AlbumContextMenu', () => {
    it('uses the total size on the album download item', () => {
      renderMenu(AlbumContextMenu, {
        id: 'al1',
        name: 'Album',
        duration: 100,
        size: 1024 * 1024,
      })
      expect(screen.getByText('ra.action.download (1 MB)')).toBeInTheDocument()
    })
  })

  describe('refresh metadata', () => {
    it('shows the item for admins on the album menu', () => {
      renderMenu(AlbumContextMenu, { id: 'al1', name: 'Album', songCount: 1 })
      expect(
        screen.getByText('resources.album.actions.refresh'),
      ).toBeInTheDocument()
    })

    // Menu order comes from key insertion order in the options object, so it is easy to
    // change by accident when adding an entry.
    it('places the item directly above Get Info', () => {
      renderMenu(AlbumContextMenu, { id: 'al1', name: 'Album', songCount: 1 })
      const labels = screen
        .getAllByRole('menuitem')
        .map((item) => item.textContent)
      const refreshAt = labels.indexOf('resources.album.actions.refresh')
      const infoAt = labels.indexOf('resources.album.actions.info')

      expect(refreshAt).toBeGreaterThanOrEqual(0)
      expect(infoAt).toEqual(refreshAt + 1)
    })

    it('shows the item for admins on the artist menu', () => {
      renderMenu(ArtistContextMenu, { id: 'ar1', name: 'Artist', stats: {} })
      expect(
        screen.getByText('resources.album.actions.refresh'),
      ).toBeInTheDocument()
    })

    it('hides the item for regular users', () => {
      mockPermissions.value = 'regular'
      renderMenu(AlbumContextMenu, { id: 'al1', name: 'Album', songCount: 1 })
      expect(
        screen.queryByText('resources.album.actions.refresh'),
      ).not.toBeInTheDocument()
    })

    it('calls refreshMetadata with the resource and id', () => {
      mockRefreshMetadata.mockResolvedValue({})
      renderMenu(AlbumContextMenu, { id: 'al1', name: 'Album', songCount: 1 })
      fireEvent.click(screen.getByText('resources.album.actions.refresh'))
      expect(mockRefreshMetadata).toHaveBeenCalledWith('album', 'al1')
    })
  })

  describe('delete from library', () => {
    const album = { id: 'al1', name: 'Album', songCount: 1 }

    beforeEach(() => {
      mockConfig.enableDeletion = true
    })

    it('shows the item for admins on the album menu', () => {
      renderMenu(AlbumContextMenu, album)
      expect(
        screen.getByText('resources.album.actions.deleteFromLibrary'),
      ).toBeInTheDocument()
    })

    it('hides the item for regular users', () => {
      mockPermissions.value = 'regular'
      renderMenu(AlbumContextMenu, album)
      expect(
        screen.queryByText('resources.album.actions.deleteFromLibrary'),
      ).not.toBeInTheDocument()
    })

    it('hides the item when the server has deletion turned off', () => {
      mockConfig.enableDeletion = false
      renderMenu(AlbumContextMenu, album)
      expect(
        screen.queryByText('resources.album.actions.deleteFromLibrary'),
      ).not.toBeInTheDocument()
    })

    // Every other action on the per-disc header menu is scoped to that disc, but deletion
    // is album-wide, so it must not appear there.
    it('is not offered on the per-disc header menu', () => {
      renderMenu(AlbumContextMenu, album, { discNumber: 2 })
      expect(
        screen.queryByText('resources.album.actions.deleteFromLibrary'),
      ).not.toBeInTheDocument()
    })

    it('is still offered on the album menu proper', () => {
      renderMenu(AlbumContextMenu, album)
      expect(
        screen.getByText('resources.album.actions.deleteFromLibrary'),
      ).toBeInTheDocument()
    })

    // Deleting a whole artist would take every album with it, and the server has no
    // endpoint for it either.
    it('is not offered on the artist menu', () => {
      renderMenu(ArtistContextMenu, { id: 'ar1', name: 'Artist', stats: {} })
      expect(
        screen.queryByText('resources.album.actions.deleteFromLibrary'),
      ).not.toBeInTheDocument()
    })

    it('asks for confirmation instead of deleting straight away', () => {
      renderMenu(AlbumContextMenu, album)
      fireEvent.click(
        screen.getByText('resources.album.actions.deleteFromLibrary'),
      )
      expect(mockDeleteFromLibrary).not.toHaveBeenCalled()
      expect(
        screen.getByText('message.deleteFromLibraryAlbumTitle'),
      ).toBeInTheDocument()
    })

    it('calls the endpoint with the album id once confirmed', async () => {
      mockDeleteFromLibrary.mockResolvedValue({ data: { count: 1 } })
      renderMenu(AlbumContextMenu, album)
      fireEvent.click(
        screen.getByText('resources.album.actions.deleteFromLibrary'),
      )
      fireEvent.click(screen.getByText('ra.action.confirm'))

      await waitFor(() =>
        expect(mockDeleteFromLibrary).toHaveBeenCalledWith('album', ['al1']),
      )
    })

    it('does not call the endpoint when the dialog is cancelled', () => {
      renderMenu(AlbumContextMenu, album)
      fireEvent.click(
        screen.getByText('resources.album.actions.deleteFromLibrary'),
      )
      fireEvent.click(screen.getByText('ra.action.cancel'))
      expect(mockDeleteFromLibrary).not.toHaveBeenCalled()
    })
  })
})
