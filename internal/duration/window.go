package duration

import (
	"errors"
	"time"

	"github.com/safedep/dry/usefulerror"
)

// Window turns the --since and --until flags of a list command into a time
// range. Both are durations before now, and zero means no bound. after is
// nil when since is zero, and before is nil when until is zero. More than
// one list command reads these flags, so they all refuse the same input with
// the same message.
func Window(since, until time.Duration, now time.Time) (after, before *time.Time, err error) {
	if since < 0 || until < 0 {
		return nil, nil, badWindow(errors.New("--since and --until must not be negative"), "Use a duration such as 7d")
	}
	if since > 0 && until >= since {
		return nil, nil, badWindow(errors.New("--until must be shorter than --since"),
			"As in --since 7d --until 1d for the time from 7 days ago to 1 day ago")
	}
	if since > 0 {
		t := now.Add(-since)
		after = &t
	}
	if until > 0 {
		t := now.Add(-until)
		before = &t
	}
	return after, before, nil
}

func badWindow(cause error, help string) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError(cause.Error()).
		WithHelp(help).
		Wrap(cause)
}
