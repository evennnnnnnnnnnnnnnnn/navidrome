import { evaluatePassword, STRONG } from '../utils/passwordStrength'

/**
 * Renders a non-strong password result as a single form error string.
 * Returns undefined when the password is strong, or when there is nothing to
 * check — an absent password means "not changing it", which is not an error here.
 *
 * The server enforces the same rule (utils/pwdstrength via
 * persistence.ValidatePasswordStrength), so this only saves a round trip; it is
 * not the thing standing between a weak password and the database.
 */
export const validatePasswordStrength = (password, values, translate) => {
  if (!password) return undefined

  const { level, reasons } = evaluatePassword(
    password,
    values?.userName,
    values?.email,
  )
  if (level === STRONG) return undefined

  const detail = reasons
    .map((r) => translate(`resources.user.passwordReason.${r}`))
    .join(' · ')
  const label = translate(`resources.user.passwordStrength.${level}`)
  return detail ? `${label} — ${detail}` : label
}

// User form validation utilities
export const validateUserForm = (values, translate) => {
  const errors = {}

  // Only require library selection for non-admin users
  if (!values.isAdmin) {
    // Check both libraryIds (array of IDs) and libraries (array of objects)
    const hasLibraryIds = values.libraryIds && values.libraryIds.length > 0
    const hasLibraries = values.libraries && values.libraries.length > 0

    if (!hasLibraryIds && !hasLibraries) {
      errors.libraryIds = translate(
        'resources.user.validation.librariesRequired',
      )
    }
  }

  const passwordError = validatePasswordStrength(
    values.password,
    values,
    translate,
  )
  if (passwordError) {
    errors.password = passwordError
  }

  if (values.scrobbleFilter && values.scrobbleFilter.trim() !== '') {
    try {
      JSON.parse(values.scrobbleFilter)
    } catch {
      errors.scrobbleFilter = translate(
        'resources.user.validation.invalidScrobbleFilter',
      )
    }
  }

  return errors
}
