package wtp

import "fmt"

// previewDecision is what to do about a preview already recorded on a port.
type previewDecision int

const (
	// decisionAlreadyServing means the requested preview is running and listening.
	decisionAlreadyServing previewDecision = iota
	// decisionRestart means the same worktree is being previewed again.
	decisionRestart
	// decisionSwitch means another worktree of this repository holds the port.
	decisionSwitch
	// decisionClearStale means the recorded process is gone and the state is dead.
	decisionClearStale
)

// classifyExisting decides how a recorded preview affects a new launch.
//
// A repository previews one worktree at a time on one stable port. Pointing the
// port at another worktree of the same repository stops the running preview and
// starts the new one. A preview owned by a different repository is never touched.
func classifyExisting(existing PreviewState, target Target, alive, listening bool) (previewDecision, error) {
	if !alive {
		if processGroupAlive(existing.PGID) {
			return 0, fmt.Errorf(
				"preview state for port %d no longer matches PID %d; refusing to signal an unverified process group",
				target.Port, existing.PID)
		}
		return decisionClearStale, nil
	}
	if existing.CommonDir != target.CommonDir {
		return 0, fmt.Errorf("port %d is managed by another repository: %s", target.Port, existing.Repository)
	}
	if existing.TargetApp != target.TargetApp {
		return decisionSwitch, nil
	}
	if existing.Branch == target.Branch && listening {
		return decisionAlreadyServing, nil
	}
	return decisionRestart, nil
}
