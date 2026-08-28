package model

import (
	"fmt"
	"math"
	"time"
)

// ReviewGrade is one of the four Anki-style answer buttons a review session offers. The scheduler
// transition lives server-side (ApplyGrade) so every client sees the same schedule.
type ReviewGrade string

const (
	ReviewGradeAgain ReviewGrade = "again"
	ReviewGradeHard  ReviewGrade = "hard"
	ReviewGradeGood  ReviewGrade = "good"
	ReviewGradeEasy  ReviewGrade = "easy"
)

// ParseReviewGrade validates a wire-supplied grade string.
func ParseReviewGrade(s string) (ReviewGrade, error) {
	switch g := ReviewGrade(s); g {
	case ReviewGradeAgain, ReviewGradeHard, ReviewGradeGood, ReviewGradeEasy:
		return g, nil
	default:
		return "", fmt.Errorf("%w: invalid review grade %q", ErrValidation, s)
	}
}

// SM-2-style scheduler constants. Ease starts at 2.5 and can never drop below 1.3 (the SM-2
// floor); "again" answers relearn after a short delay instead of a full day so a failed card can
// be retried within the same session.
const (
	ReviewDefaultEase        = 2.5
	reviewMinEase            = 1.3
	reviewAgainEasePenalty   = 0.20
	reviewHardEasePenalty    = 0.15
	reviewEasyEaseBonus      = 0.15
	reviewAgainDelay         = 10 * time.Minute
	reviewHardIntervalFactor = 1.2
	reviewEasyIntervalBonus  = 1.3
	// reviewMaxIntervalDays caps geometric interval growth (100 years). Uncapped, repeated
	// easy/good grades overflow the float-days -> time.Duration conversion (~106751 days) and
	// wrap due_at into the past.
	reviewMaxIntervalDays = 36500
)

// MusicCardReview is the per-card SRS scheduling state. One row per card (unique card_id); it has
// no user_id column of its own - ownership is transitive through CardID, exactly like
// MusicCardSnippet. A card with no review row is "new": the row is created by the first grade.
type MusicCardReview struct {
	ID              string    `structs:"id"               json:"id"`
	CardID          string    `structs:"card_id"          json:"card_id"`
	DueAt           time.Time `structs:"due_at"           json:"due_at"`
	IntervalDays    float64   `structs:"interval_days"    json:"interval_days"`
	EaseFactor      float64   `structs:"ease_factor"      json:"ease_factor"`
	RepetitionCount int       `structs:"repetition_count" json:"repetition_count"`
	LapseCount      int       `structs:"lapse_count"      json:"lapse_count"`
	LastReviewedAt  time.Time `structs:"last_reviewed_at" json:"last_reviewed_at"`
	CreatedAt       time.Time `structs:"created_at"       json:"created_at"`
	UpdatedAt       time.Time `structs:"updated_at"       json:"updated_at"`
}

type MusicCardReviews []MusicCardReview

// NewMusicCardReview returns the state a card is in before its first grade: due immediately with
// the SM-2 default ease and no accumulated interval.
func NewMusicCardReview(cardID string, now time.Time) *MusicCardReview {
	return &MusicCardReview{
		CardID:     cardID,
		DueAt:      now,
		EaseFactor: ReviewDefaultEase,
	}
}

// ApplyGrade applies one SM-2-style transition to r, in place. It is a pure function of
// (r, grade, now) - no clock or database access - so the schedule is deterministic and unit
// testable.
//
// The transition follows classic SM-2 (repetition intervals 1d, 6d, then previous*ease) with the
// Anki-style four-button refinements: "again" is a lapse that resets the repetition streak and
// re-queues the card after a short delay; "hard" advances the streak but grows the interval by a
// flat 1.2x instead of the ease factor; "easy" applies a 1.3x bonus on top of the ease growth.
// Ease is adjusted per grade (-0.20 again, -0.15 hard, +0 good, +0.15 easy) and floored at 1.3.
func (r *MusicCardReview) ApplyGrade(grade ReviewGrade, now time.Time) error {
	switch grade {
	case ReviewGradeAgain:
		if r.RepetitionCount > 0 {
			r.LapseCount++
		}
		r.RepetitionCount = 0
		r.EaseFactor = math.Max(reviewMinEase, r.EaseFactor-reviewAgainEasePenalty)
		r.IntervalDays = 0
		r.DueAt = now.Add(reviewAgainDelay)
	case ReviewGradeHard:
		r.RepetitionCount++
		r.EaseFactor = math.Max(reviewMinEase, r.EaseFactor-reviewHardEasePenalty)
		if r.RepetitionCount == 1 {
			r.IntervalDays = 1
		} else {
			r.IntervalDays = clampInterval(r.IntervalDays * reviewHardIntervalFactor)
		}
		r.DueAt = now.Add(daysToDuration(r.IntervalDays))
	case ReviewGradeGood:
		r.RepetitionCount++
		switch r.RepetitionCount {
		case 1:
			r.IntervalDays = 1
		case 2:
			r.IntervalDays = 6
		default:
			r.IntervalDays = clampInterval(r.IntervalDays * r.EaseFactor)
		}
		r.DueAt = now.Add(daysToDuration(r.IntervalDays))
	case ReviewGradeEasy:
		r.RepetitionCount++
		r.EaseFactor += reviewEasyEaseBonus
		switch r.RepetitionCount {
		case 1:
			r.IntervalDays = 4
		case 2:
			r.IntervalDays = 6 * reviewEasyIntervalBonus
		default:
			r.IntervalDays = clampInterval(r.IntervalDays * r.EaseFactor * reviewEasyIntervalBonus)
		}
		r.DueAt = now.Add(daysToDuration(r.IntervalDays))
	default:
		return fmt.Errorf("%w: invalid review grade %q", ErrValidation, grade)
	}
	r.LastReviewedAt = now
	return nil
}

func clampInterval(days float64) float64 {
	return math.Min(math.Max(1, days), reviewMaxIntervalDays)
}

func daysToDuration(days float64) time.Duration {
	return time.Duration(days * 24 * float64(time.Hour))
}

// MusicCardReviewRepository is a resource repository whose ownership is scoped transitively
// through the parent MusicCard's user_id, exactly like MusicCardSnippetRepository: every method
// must verify the caller owns (or is admin of) CardID's card. The generic REST exposure is
// read-only - all writes go through the grade endpoint so the transition function is the only
// path that mutates schedule state.
type MusicCardReviewRepository interface {
	ResourceRepository
	CountAll(options ...QueryOptions) (int64, error)
	Get(id string) (*MusicCardReview, error)
	GetAll(options ...QueryOptions) (MusicCardReviews, error)
	// GetByCardID returns the review state for one owned card, or ErrNotFound when the card is
	// still "new" (never graded) or not visible to the caller.
	GetByCardID(cardID string) (*MusicCardReview, error)
	// Put upserts by the card_id natural key, verifying card ownership first.
	Put(rev *MusicCardReview) error
}
