import React from 'react'
import { render, fireEvent, screen, waitFor } from '@testing-library/react'
import { TestContext } from 'ra-test'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { SongContextMenu } from './SongContextMenu'
import subsonic from '../subsonic'

vi.mock('../dataProvider', () => ({
  httpClient: vi.fn(),
}))

vi.mock('../subsonic', () => ({
  default: { getSimilarSongs2: vi.fn() },
}))

const { mockConfig, mockPermissions, mockDeleteFromLibrary } = vi.hoisted(
  () => ({
    mockConfig: {
      enableDownloads: true,
      enableFavourites: true,
      enableSharing: true,
      enableExternalServices: true,
      enableDeletion: false,
    },
    mockPermissions: { value: 'admin' },
    mockDeleteFromLibrary: vi.fn(),
  }),
)

vi.mock('../config', () => ({ default: mockConfig }))

const mockDispatch = vi.fn()
vi.mock('react-redux', () => ({ useDispatch: () => mockDispatch }))

const getPlaylistsMock = vi.fn()
const mockNotify = vi.fn()

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useNotify: () => mockNotify,
    usePermissions: () => ({ permissions: mockPermissions.value }),
    useRefresh: () => vi.fn(),
    useRedirect: () => (url) => {
      window.location.hash = `#${url}`
    },
    useDataProvider: () => ({
      getPlaylists: getPlaylistsMock,
      deleteFromLibrary: mockDeleteFromLibrary,
      inspect: vi.fn().mockResolvedValue({
        data: { rawTags: {} },
      }),
    }),
  }
})

describe('SongContextMenu', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.location.hash = ''
    mockConfig.enableDeletion = false
    mockPermissions.value = 'admin'
    getPlaylistsMock.mockResolvedValue({
      data: [{ id: 'pl1', name: 'Pl 1' }],
    })
    subsonic.getSimilarSongs2.mockResolvedValue({
      json: {
        'subsonic-response': {
          status: 'ok',
          similarSongs2: { song: [{ id: 's1' }] },
        },
      },
    })
  })

  it('navigates to playlist when selected', async () => {
    render(
      <TestContext>
        <SongContextMenu record={{ id: 'song1', size: 1 }} resource="song" />
      </TestContext>,
    )
    fireEvent.click(screen.getAllByRole('button')[1])
    await waitFor(() =>
      screen.getByText(/resources\.song\.actions\.showInPlaylist/),
    )
    fireEvent.click(
      screen.getByText(/resources\.song\.actions\.showInPlaylist/),
    )
    await waitFor(() => screen.getByText('Pl 1'))
    fireEvent.click(screen.getByText('Pl 1'))
    expect(window.location.hash).toBe('#/playlist/pl1/show')
  })

  it('stops event propagation when playlist submenu is closed', async () => {
    const mockOnClick = vi.fn()
    render(
      <TestContext>
        <div onClick={mockOnClick}>
          <SongContextMenu record={{ id: 'song1', size: 1 }} resource="song" />
        </div>
      </TestContext>,
    )

    // Open main menu
    fireEvent.click(screen.getAllByRole('button')[1])
    await waitFor(() =>
      screen.getByText(/resources\.song\.actions\.showInPlaylist/),
    )

    // Open playlist submenu
    fireEvent.click(
      screen.getByText(/resources\.song\.actions\.showInPlaylist/),
    )
    await waitFor(() => screen.getByText('Pl 1'))

    // Click outside the playlist submenu (should close it without triggering parent click)
    fireEvent.click(document.body)

    expect(mockOnClick).not.toHaveBeenCalled()
  })

  it('does nothing when "Show in Playlist" is disabled', async () => {
    getPlaylistsMock.mockResolvedValue({ data: [] })
    const mockOnClick = vi.fn()
    render(
      <TestContext>
        <div onClick={mockOnClick}>
          <SongContextMenu record={{ id: 'song1', size: 1 }} resource="song" />
        </div>
      </TestContext>,
    )

    fireEvent.click(screen.getAllByRole('button')[1])
    await waitFor(() =>
      screen.getByText(/resources\.song\.actions\.showInPlaylist/),
    )

    fireEvent.click(
      screen.getByText(/resources\.song\.actions\.showInPlaylist/),
    )
    expect(mockOnClick).not.toHaveBeenCalled()
  })

  describe('Instant Mix action', () => {
    it('calls getSimilarSongs2 with song id and shows loading notification', async () => {
      render(
        <TestContext>
          <SongContextMenu record={{ id: 'song1', size: 1 }} resource="song" />
        </TestContext>,
      )

      fireEvent.click(screen.getAllByRole('button')[1])
      await waitFor(() =>
        screen.getByText(/resources\.song\.actions\.instantMix/),
      )
      fireEvent.click(screen.getByText(/resources\.song\.actions\.instantMix/))

      // Verify loading notification is shown
      expect(mockNotify).toHaveBeenCalledWith('message.startingInstantMix', {
        type: 'info',
      })

      await waitFor(() =>
        expect(subsonic.getSimilarSongs2).toHaveBeenCalledWith('song1', 100),
      )
      expect(mockDispatch).toHaveBeenCalled()
    })

    it('plays seed song first followed by similar songs', async () => {
      const seedRecord = { id: 'song1', title: 'Seed Song', size: 1 }
      render(
        <TestContext>
          <SongContextMenu record={seedRecord} resource="song" />
        </TestContext>,
      )

      fireEvent.click(screen.getAllByRole('button')[1])
      await waitFor(() =>
        screen.getByText(/resources\.song\.actions\.instantMix/),
      )
      fireEvent.click(screen.getByText(/resources\.song\.actions\.instantMix/))

      await waitFor(() => expect(mockDispatch).toHaveBeenCalled())

      // Verify dispatch was called with playTracks action
      const dispatchCall = mockDispatch.mock.calls.find(
        (call) => call[0]?.type === 'PLAYER_PLAY_TRACKS',
      )
      expect(dispatchCall).toBeDefined()

      // Verify seed song is first (id property contains the first song to play)
      const { id, data } = dispatchCall[0]
      expect(id).toBe('song1')
      // Verify seed song data is included
      expect(data['song1']).toBeDefined()
    })

    it('uses mediaFileId when available (playlist context)', async () => {
      render(
        <TestContext>
          <SongContextMenu
            record={{
              id: 'playlistTrackId',
              mediaFileId: 'actualSongId',
              size: 1,
            }}
            resource="song"
          />
        </TestContext>,
      )

      fireEvent.click(screen.getAllByRole('button')[1])
      await waitFor(() =>
        screen.getByText(/resources\.song\.actions\.instantMix/),
      )
      fireEvent.click(screen.getByText(/resources\.song\.actions\.instantMix/))

      await waitFor(() =>
        expect(subsonic.getSimilarSongs2).toHaveBeenCalledWith(
          'actualSongId',
          100,
        ),
      )

      await waitFor(() => expect(mockDispatch).toHaveBeenCalled())

      // Verify the mediaFileId is used as the seed song id
      const dispatchCall = mockDispatch.mock.calls.find(
        (call) => call[0]?.type === 'PLAYER_PLAY_TRACKS',
      )
      expect(dispatchCall).toBeDefined()
      const { id, data } = dispatchCall[0]
      expect(id).toBe('actualSongId')
      // Verify seed song data is included
      expect(data['actualSongId']).toBeDefined()
    })
  })
  describe('delete from library', () => {
    const renderMenu = (record) => {
      render(
        <TestContext>
          <SongContextMenu record={record} resource="song" />
        </TestContext>,
      )
      fireEvent.click(screen.getAllByRole('button')[1])
    }

    beforeEach(() => {
      mockConfig.enableDeletion = true
    })

    it('shows the item for admins', () => {
      renderMenu({ id: 'song1', size: 1 })
      expect(
        screen.getByText('resources.song.actions.deleteFromLibrary'),
      ).toBeInTheDocument()
    })

    it('hides the item for regular users', () => {
      mockPermissions.value = 'regular'
      renderMenu({ id: 'song1', size: 1 })
      expect(
        screen.queryByText('resources.song.actions.deleteFromLibrary'),
      ).not.toBeInTheDocument()
    })

    it('hides the item when the server has deletion turned off', () => {
      mockConfig.enableDeletion = false
      renderMenu({ id: 'song1', size: 1 })
      expect(
        screen.queryByText('resources.song.actions.deleteFromLibrary'),
      ).not.toBeInTheDocument()
    })

    it('asks for confirmation instead of deleting straight away', () => {
      renderMenu({ id: 'song1', size: 1 })
      fireEvent.click(
        screen.getByText('resources.song.actions.deleteFromLibrary'),
      )
      expect(mockDeleteFromLibrary).not.toHaveBeenCalled()
      expect(
        screen.getByText('message.deleteFromLibrarySongTitle'),
      ).toBeInTheDocument()
    })

    it('deletes the song once confirmed', async () => {
      mockDeleteFromLibrary.mockResolvedValue({ data: { count: 1 } })
      renderMenu({ id: 'song1', size: 1 })
      fireEvent.click(
        screen.getByText('resources.song.actions.deleteFromLibrary'),
      )
      fireEvent.click(screen.getByText('ra.action.confirm'))

      await waitFor(() =>
        expect(mockDeleteFromLibrary).toHaveBeenCalledWith('song', ['song1']),
      )
    })

    // In a playlist the row id is the playlist-track id; the media file id is the one the
    // server can actually delete.
    it('uses mediaFileId when the row carries one', async () => {
      mockDeleteFromLibrary.mockResolvedValue({ data: { count: 1 } })
      renderMenu({ id: 'playlistTrack1', mediaFileId: 'actualSongId', size: 1 })
      fireEvent.click(
        screen.getByText('resources.song.actions.deleteFromLibrary'),
      )
      fireEvent.click(screen.getByText('ra.action.confirm'))

      await waitFor(() =>
        expect(mockDeleteFromLibrary).toHaveBeenCalledWith('song', [
          'actualSongId',
        ]),
      )
    })

    it('does not delete when the dialog is cancelled', () => {
      renderMenu({ id: 'song1', size: 1 })
      fireEvent.click(
        screen.getByText('resources.song.actions.deleteFromLibrary'),
      )
      fireEvent.click(screen.getByText('ra.action.cancel'))
      expect(mockDeleteFromLibrary).not.toHaveBeenCalled()
    })
  })
})
