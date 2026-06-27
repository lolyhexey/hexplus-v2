// Package progress renders an animated progress bar + spinner while
// a sequence of steps runs. Each step is a label + a work function.
//
// Output format (one overwritten line per step):
//
//	[████████████░░░░░░░░] 60%  ขั้นที่ 3/5: สร้าง PKI...  ⠸
//
// On completion the line is replaced with a ✓ checkmark and the next
// step begins. The whole thing collapses to a clean list of ticked
// steps when done, with no leftover spinner garbage.
package progress

import (
	"fmt"
	"os"
	"time"
)

const (
	barWidth   = 20
	tickMs     = 80
	fillChar   = '█'
	emptyChar  = '░'
	doneChar   = "✓"
	failChar   = "✗"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Step is one unit of work: a display label and a function to run.
type Step struct {
	Label string
	Work  func() error
}

// Run executes steps in order, animating a progress bar while each
// step runs. Returns the first error encountered (subsequent steps are
// skipped).
func Run(steps []Step) error {
	total := len(steps)
	for i, s := range steps {
		startPct := pct(i, total)
		endPct := pct(i+1, total)

		errCh := make(chan error, 1)
		go func(fn func() error) { errCh <- fn() }(s.Work)

		err := animate(i+1, total, startPct, endPct, s.Label, errCh)
		if err != nil {
			// Print failing step in red then return.
			printLine(endPct, i+1, total, failChar+"\033[1;31m", s.Label, "")
			fmt.Fprintln(os.Stdout)
			return err
		}
		// Print completed step (static, green tick).
		printLine(endPct, i+1, total, "\033[1;32m"+doneChar, s.Label, "")
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// animate writes the animated line until errCh receives, then returns.
func animate(stepNo, total, startPct, endPct int, label string, errCh <-chan error) error {
	ticker := time.NewTicker(tickMs * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	for {
		select {
		case err := <-errCh:
			return err
		case <-ticker.C:
			// Interpolate pct within the step so the bar creeps forward
			// even though we don't know the real completion fraction.
			// Cap at endPct-1 so it snaps to endPct only when done.
			creep := startPct + (endPct-startPct)*frame/(frame+8)
			if creep >= endPct {
				creep = endPct - 1
			}
			spin := spinFrames[frame%len(spinFrames)]
			printLine(creep, stepNo, total, "\033[1;33m→", label, spin)
			frame++
		}
	}
}

// printLine renders one progress line using \r to overwrite in place.
func printLine(p, stepNo, total int, icon, label, spin string) {
	bar := makeBar(p)
	fmt.Printf("\r\033[1;33m[%s\033[1;33m] \033[1;37m%3d%%  %s \033[1;37mขั้นที่ %d/%d: \033[1;36m%s\033[1;33m  %s\033[K",
		bar, p, icon, stepNo, total, label, spin)
}

// makeBar builds a color-coded bar string of width barWidth.
func makeBar(p int) string {
	filled := barWidth * p / 100
	if filled > barWidth {
		filled = barWidth
	}
	s := ""
	for i := 0; i < barWidth; i++ {
		if i < filled {
			s += "\033[1;32m" + string(fillChar)
		} else {
			s += "\033[0;37m" + string(emptyChar)
		}
	}
	return s
}

func pct(step, total int) int {
	if total == 0 {
		return 100
	}
	return step * 100 / total
}
