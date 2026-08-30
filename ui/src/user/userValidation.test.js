import { describe, it, expect, vi } from 'vitest'
import { validateUserForm, validatePasswordStrength } from './userValidation'

describe('User Validation Utilities', () => {
  const mockTranslate = vi.fn((key) => key)

  describe('validateUserForm', () => {
    it('should not return errors for admin users', () => {
      const values = {
        isAdmin: true,
        libraryIds: [],
      }
      const errors = validateUserForm(values, mockTranslate)
      expect(errors).toEqual({})
    })

    it('should not return errors for non-admin users with libraries', () => {
      const values = {
        isAdmin: false,
        libraryIds: [1, 2, 3],
      }
      const errors = validateUserForm(values, mockTranslate)
      expect(errors).toEqual({})
    })

    it('should return error for non-admin users without libraries', () => {
      const values = {
        isAdmin: false,
        libraryIds: [],
      }
      const errors = validateUserForm(values, mockTranslate)
      expect(errors.libraryIds).toBe(
        'resources.user.validation.librariesRequired',
      )
    })

    it('should return error for non-admin users with undefined libraryIds', () => {
      const values = {
        isAdmin: false,
      }
      const errors = validateUserForm(values, mockTranslate)
      expect(errors.libraryIds).toBe(
        'resources.user.validation.librariesRequired',
      )
    })

    it('should not return errors for non-admin users with libraries array', () => {
      const values = {
        isAdmin: false,
        libraries: [
          { id: 1, name: 'Library 1' },
          { id: 2, name: 'Library 2' },
        ],
      }
      const errors = validateUserForm(values, mockTranslate)
      expect(errors).toEqual({})
    })

    it('should return error for non-admin users with empty libraries array', () => {
      const values = {
        isAdmin: false,
        libraries: [],
      }
      const errors = validateUserForm(values, mockTranslate)
      expect(errors.libraryIds).toBe(
        'resources.user.validation.librariesRequired',
      )
    })
  })

  describe('scrobbleFilter validation', () => {
    it('accepts an empty filter', () => {
      const errors = validateUserForm({ isAdmin: true }, mockTranslate)
      expect(errors.scrobbleFilter).toBeUndefined()
    })
    it('accepts valid JSON', () => {
      const errors = validateUserForm(
        { isAdmin: true, scrobbleFilter: '{"all":[{"lt":{"rating":4}}]}' },
        mockTranslate,
      )
      expect(errors.scrobbleFilter).toBeUndefined()
    })
    it('rejects malformed JSON', () => {
      const errors = validateUserForm(
        { isAdmin: true, scrobbleFilter: '{broken' },
        mockTranslate,
      )
      expect(errors.scrobbleFilter).toEqual(
        'resources.user.validation.invalidScrobbleFilter',
      )
    })
  })
})

describe('password strength validation', () => {
  const translate = vi.fn((key) => key)

  describe('validatePasswordStrength', () => {
    it('passes a strong password', () => {
      expect(
        validatePasswordStrength('Correct-Horse-Battery-9', {}, translate),
      ).toBeUndefined()
    })

    it('says nothing when there is no password to check', () => {
      // An absent password means "not changing it", which is not an error.
      for (const empty of ['', null, undefined]) {
        expect(validatePasswordStrength(empty, {}, translate)).toBeUndefined()
      }
    })

    it('reports the level and the reasons for a weak password', () => {
      const err = validatePasswordStrength('secret', {}, translate)
      expect(err).toContain('resources.user.passwordStrength.weak')
      expect(err).toContain('resources.user.passwordReason.tooShort')
    })

    it('reports medium as still not good enough', () => {
      const err = validatePasswordStrength('Tr0ub4dor&3', {}, translate)
      expect(err).toContain('resources.user.passwordStrength.medium')
      expect(err).toContain('resources.user.passwordReason.needsLength')
    })

    it('takes the username and email from the surrounding form values', () => {
      const values = { userName: 'even', email: 'yiwenz@example.com' }
      expect(
        validatePasswordStrength('even-Strong-Pass-99', values, translate),
      ).toContain('resources.user.passwordReason.containsUsername')
      expect(
        validatePasswordStrength('yiwenz-Music-2026!', values, translate),
      ).toContain('resources.user.passwordReason.containsEmail')
    })

    it('tolerates missing form values', () => {
      expect(
        validatePasswordStrength(
          'Correct-Horse-Battery-9',
          undefined,
          translate,
        ),
      ).toBeUndefined()
    })
  })

  describe('validateUserForm', () => {
    it('rejects a weak password on the form', () => {
      const errors = validateUserForm(
        { isAdmin: true, password: 'Password123!' },
        translate,
      )
      expect(errors.password).toContain(
        'resources.user.passwordReason.commonPassword',
      )
    })

    it('accepts a strong password on the form', () => {
      const errors = validateUserForm(
        { isAdmin: true, password: 'Correct-Horse-Battery-9' },
        translate,
      )
      expect(errors.password).toBeUndefined()
    })

    it('does not flag a form with no password field at all', () => {
      // Editing a name without ticking "change password" must not error.
      const errors = validateUserForm(
        { isAdmin: true, name: 'Renamed' },
        translate,
      )
      expect(errors.password).toBeUndefined()
    })
  })
})
