package analyze

import "time"

// Window is a named lookback the TUI and CLI share. Presets cover the common
// questions — this week, this month, this year — without making the user do
// calendar math.
type Window string

const (
	Window7d  Window = "7d"
	Window30d Window = "30d"
	WindowYTD Window = "ytd"
)

// Windows is the ordered chip set shown in the TUI.
var Windows = []Window{Window7d, Window30d, WindowYTD}

func (w Window) Valid() bool {
	switch w {
	case Window7d, Window30d, WindowYTD:
		return true
	}
	return false
}

func (w Window) Label() string {
	switch w {
	case Window30d:
		return "30d"
	case WindowYTD:
		return "YTD"
	default:
		return "7d"
	}
}

// Since returns the inclusive lower bound of the window relative to now.
func (w Window) Since(now time.Time) time.Time {
	now = now.Truncate(time.Second)
	switch w {
	case Window30d:
		return now.Add(-30 * 24 * time.Hour)
	case WindowYTD:
		return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	default:
		return now.Add(-7 * 24 * time.Hour)
	}
}

func (w Window) Next() Window {
	for i, candidate := range Windows {
		if candidate == w {
			return Windows[(i+1)%len(Windows)]
		}
	}
	return Window7d
}

func (w Window) Prev() Window {
	for i, candidate := range Windows {
		if candidate == w {
			return Windows[(i+len(Windows)-1)%len(Windows)]
		}
	}
	return Window7d
}

// WindowFromDays maps a --days value onto the nearest named preset so the TUI
// chips light up correctly when the CLI picks the window.
func WindowFromDays(days int, now time.Time) Window {
	if days <= 0 {
		return Window7d
	}
	ytdDays := int(now.Sub(WindowYTD.Since(now)).Hours()/24) + 1
	switch {
	case days <= 7:
		return Window7d
	case days <= 30:
		return Window30d
	case days >= ytdDays-1:
		return WindowYTD
	default:
		return Window30d
	}
}
