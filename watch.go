package main

import (
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"time"
)

type Watch struct {
	Endpoint string
	WatchDogConfig
	Interval time.Duration

	// Dynamic properties are protected with a goroutine
	k chan<- struct{}
	s <-chan WatchDogStatus
}

type WatchDogConfig struct {
	Trigger   TriggerType
	Interval_ Duration `json:"Interval"`
	OnExpiry  string
}

type WatchDogStatus struct {
	LastSeen       time.Time
	IntervalMean   Duration  `json:",omitempty"`
	IntervalStdDev Duration  `json:",omitempty"`
	Due            time.Time `json:"-"`
	MissedReports  int       `json:"-"`
}

func NewWatch(ep string, cfg WatchDogConfig, st WatchDogStatus, execArgs []string, pid int) *Watch {
	interval := time.Duration(cfg.Interval_)
	if interval == 0 {
		interval = 30 * time.Second
	}

	myExecArgs := []string{}

	if len(execArgs) > 0 && len(cfg.OnExpiry) > 0 {
		myExecArgs = append([]string{}, execArgs...)
		myExecArgs = append(myExecArgs, cfg.OnExpiry)
	}

	k := make(chan struct{})
	s := make(chan WatchDogStatus)

	w := &Watch{ep, cfg, interval, k, s}

	go w.eventHandler(st, k, s, myExecArgs, pid)

	return w
}

const meanAlpha float64 = 0.15

func (w Watch) eventHandler(status WatchDogStatus, k <-chan struct{}, s chan<- WatchDogStatus, myExecArgs []string, pid int) {
	var timer *time.Timer

	if status.LastSeen.IsZero() {
		timer = time.NewTimer(w.Interval)
		status.Due = time.Now().Add(w.Interval)
	} else {
		timer = time.NewTimer(w.Interval - time.Now().Sub(status.LastSeen))
		status.Due = status.LastSeen.Add(w.Interval)
	}

	variance := float64(status.IntervalStdDev) * float64(status.IntervalStdDev)

	for {
		select {
		case t := <-timer.C: // Timer expired
			timer.Reset(w.Interval)
			status.Due = t.Add(w.Interval)

			// Periodic triggers are expected to see expiries
			if w.Trigger == Periodic {
				status.LastSeen = t
			} else {
				status.MissedReports += 1
			}
			// Run command...
			if len(myExecArgs) > 0 {
				lsUnix := int64(0)
				if !status.LastSeen.IsZero() {
					lsUnix = status.LastSeen.Unix()
				}

				cmd := exec.Command(myExecArgs[0], myExecArgs[1:]...)
				cmd.Env = append(cmd.Env,
					fmt.Sprintf("WOOF_PID=%v", pid),
					fmt.Sprintf("WOOF_ENDPOINT=%v", w.Endpoint),
					fmt.Sprintf("WOOF_LASTSEEN=%v", lsUnix),
					fmt.Sprintf("WOOF_DUE=%v", status.Due.Unix()),
					fmt.Sprintf("WOOF_MISSEDREPORTS=%v", status.MissedReports))
				cmd.Run()
			}

		case <-k: // Kick watchdog
			now := time.Now()
			if !status.LastSeen.IsZero() {
				measuredInterval := now.Sub(status.LastSeen)

				// Following http://people.ds.cam.ac.uk/fanf2/hermes/doc/antiforgery/stats.pdf
				diff := float64(measuredInterval - time.Duration(status.IntervalMean))
				incr := meanAlpha * diff
				status.IntervalMean += Duration(incr)
				variance = (1 - meanAlpha) * (variance + diff*incr)
				status.IntervalStdDev = Duration(math.Sqrt(variance))
			}

			status.LastSeen = now
			status.MissedReports = 0

			// Re-arm the timeout
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(w.Interval)
			status.Due = status.LastSeen.Add(w.Interval)

		case s <- status:
		}
	}
}

func (w *Watch) ServeHttp(wr http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(wr, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if w.Trigger == Periodic {
		http.Error(wr, "Forbidden", http.StatusForbidden)
		return
	}

	w.k <- struct{}{}

	// Manual watchdogs should redirect to main UI
	if w.Trigger == Manual {
		http.Redirect(wr, r, "/", http.StatusSeeOther)
	} else {
		http.Error(wr, "Watchdog kicked", http.StatusOK)
	}
}

func (w Watch) Less(o Watch) bool {
	if w.Trigger != o.Trigger {
		return w.Trigger < o.Trigger
	}
	return w.Endpoint < o.Endpoint
}

func (w Watch) Status() WatchDogStatus {
	return <-w.s
}

func (w Watch) LastSeen() time.Time {
	s := <-w.s
	return s.LastSeen
}

func (w Watch) LastSeenFriendly() string {
	s := <-w.s
	return friendly(s.LastSeen)
}

func (w Watch) DueFriendly() string {
	s := <-w.s
	return friendly(s.Due)
}

func (w Watch) MissedReports() int {
	s := <-w.s
	return s.MissedReports
}

func friendly(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	u := time.Since(t)
	if u < 0 {
		u = -u
	}
	switch {
	case u <= time.Minute*30:
		return time.Until(t).String()
	case u <= time.Hour*12:
		return t.Format("15:04")
	case u <= time.Hour*24*3:
		return t.Format("Mon 15:04")
	case u <= time.Hour*24*182:
		return t.Format("Mon, 02 Jan")
	default:
		return t.Format(time.RFC3339)
	}
}

func (w Watch) StatsFriendly() string {
	s := <-w.s
	return fmt.Sprintf("Mean interval: %v\nstddev: %v", s.IntervalMean, s.IntervalStdDev)
}
