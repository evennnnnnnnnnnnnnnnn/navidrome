import React from 'react'
import PropTypes from 'prop-types'
import { makeStyles } from '@material-ui/core/styles'
import { Typography } from '@material-ui/core'
import { useTranslate } from 'react-admin'
import {
  evaluatePassword,
  WEAK,
  MEDIUM,
  STRONG,
} from '../utils/passwordStrength'

const LEVEL_COLORS = {
  [WEAK]: '#d32f2f',
  [MEDIUM]: '#ed6c02',
  [STRONG]: '#2e7d32',
}

const FILLED_SEGMENTS = { [WEAK]: 1, [MEDIUM]: 2, [STRONG]: 3 }

const useStyles = makeStyles((theme) => ({
  root: {
    marginTop: theme.spacing(0.5),
    marginBottom: theme.spacing(1),
    width: '100%',
    maxWidth: 256,
  },
  segments: {
    display: 'flex',
    gap: 4,
  },
  segment: {
    height: 4,
    flex: 1,
    borderRadius: 2,
    // Not theme.palette.action.disabledBackground: that is nearly invisible on
    // some of the bundled themes, which makes the meter look like it is missing.
    backgroundColor: theme.palette.type === 'dark' ? '#4a4a4a' : '#d5d5d5',
    transition: 'background-color 120ms ease',
  },
  label: {
    marginTop: theme.spacing(0.5),
    fontWeight: 600,
  },
  reasons: {
    display: 'block',
    lineHeight: 1.4,
  },
}))

/**
 * A weak/medium/strong meter for a password field.
 *
 * The score comes from the same algorithm the server enforces
 * (utils/pwdstrength), so "strong" here always means the server will accept it.
 * Empty input renders nothing rather than a red "too short" bar — the field being
 * untouched is not an error yet.
 */
export const PasswordStrengthMeter = ({ password, username, email }) => {
  const classes = useStyles()
  const translate = useTranslate()

  if (!password) return null

  const { level, reasons } = evaluatePassword(password, username, email)
  const filled = FILLED_SEGMENTS[level]
  const color = LEVEL_COLORS[level]

  return (
    <div className={classes.root} data-testid="password-strength-meter">
      <div className={classes.segments}>
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className={classes.segment}
            style={i < filled ? { backgroundColor: color } : undefined}
          />
        ))}
      </div>
      <Typography
        variant="caption"
        className={classes.label}
        style={{ color }}
        data-testid="password-strength-label"
      >
        {translate(`resources.user.passwordStrength.${level}`)}
      </Typography>
      {reasons.length > 0 && (
        <Typography
          variant="caption"
          color="textSecondary"
          className={classes.reasons}
          data-testid="password-strength-reasons"
        >
          {reasons
            .map((r) => translate(`resources.user.passwordReason.${r}`))
            .join(' · ')}
        </Typography>
      )}
    </div>
  )
}

PasswordStrengthMeter.propTypes = {
  password: PropTypes.string,
  username: PropTypes.string,
  email: PropTypes.string,
}

export default PasswordStrengthMeter
