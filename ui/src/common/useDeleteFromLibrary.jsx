import { useCallback, useEffect, useRef, useState } from 'react'
import {
  useDataProvider,
  useNotify,
  usePermissions,
  useRefresh,
} from 'react-admin'
import config from '../config'

/**
 * Shared plumbing for the admin-only "delete from library" action, which moves the
 * underlying files into the server's trash folder and drops their rows.
 *
 * The affordance is hidden unless the server has the feature enabled *and* the current
 * user is an admin. The endpoint enforces both independently — hiding it here just avoids
 * offering a menu entry that could only ever answer 403.
 *
 * @param {'song'|'album'} kind which endpoint to call
 */
export const useDeleteFromLibrary = (kind) => {
  const { permissions } = usePermissions()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [pendingIds, setPendingIds] = useState(null)
  const [loading, setLoading] = useState(false)
  const mounted = useRef(true)

  useEffect(
    () => () => {
      mounted.current = false
    },
    [],
  )

  const enabled = Boolean(config.enableDeletion) && permissions === 'admin'

  const requestDelete = useCallback((ids) => {
    const list = (Array.isArray(ids) ? ids : [ids]).filter(Boolean)
    if (list.length === 0) {
      return
    }
    setPendingIds(list)
  }, [])

  const cancel = useCallback(() => setPendingIds(null), [])

  const confirm = useCallback(async () => {
    if (!pendingIds) {
      return
    }
    setLoading(true)
    try {
      const { data } = await dataProvider.deleteFromLibrary(kind, pendingIds)
      notify('message.deleteFromLibrarySuccess', {
        type: 'info',
        messageArgs: { smart_count: data?.count ?? pendingIds.length },
      })
      refresh()
    } catch (error) {
      // The endpoint answers with {"message": "..."} explaining the refusal (feature off,
      // unsafe path, partial batch). react-admin surfaces that as error.message.
      notify('message.deleteFromLibraryError', {
        type: 'warning',
        multiLine: true,
        duration: 0,
        messageArgs: { error: error?.message || '' },
      })
    } finally {
      if (mounted.current) {
        setLoading(false)
        setPendingIds(null)
      }
    }
  }, [dataProvider, kind, notify, pendingIds, refresh])

  return {
    cancel,
    confirm,
    enabled,
    isOpen: pendingIds !== null,
    loading,
    requestDelete,
  }
}
