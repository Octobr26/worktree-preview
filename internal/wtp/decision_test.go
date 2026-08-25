package wtp

import (
	"strings"
	"syscall"
	"testing"
)

// deadProcessGroup is above the kernel PID ceiling, so no process group matches it.
const deadProcessGroup = 1 << 24

func baseTarget() Target {
	return Target{
		CommonDir:      "/repo/.git",
		TargetWorktree: "/repo/feature",
		TargetApp:      "/repo/feature",
		Branch:         "feature",
		Port:           3000,
	}
}

func baseState() PreviewState {
	return PreviewState{
		Version:        1,
		Port:           3000,
		CommonDir:      "/repo/.git",
		Repository:     "/repo",
		TargetWorktree: "/repo/feature",
		TargetApp:      "/repo/feature",
		Branch:         "feature",
		PID:            4242,
		PGID:           4242,
	}
}

func TestClassifyExistingAlreadyServing(t *testing.T) {
	decision, err := classifyExisting(baseState(), baseTarget(), true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != decisionAlreadyServing {
		t.Fatalf("got decision %v, want decisionAlreadyServing", decision)
	}
}

func TestClassifyExistingRestartsWhenPortNotListening(t *testing.T) {
	decision, err := classifyExisting(baseState(), baseTarget(), true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != decisionRestart {
		t.Fatalf("got decision %v, want decisionRestart", decision)
	}
}

func TestClassifyExistingRestartsWhenSameWorktreeChangedBranch(t *testing.T) {
	state := baseState()
	state.Branch = "old-branch"
	decision, err := classifyExisting(state, baseTarget(), true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != decisionRestart {
		t.Fatalf("got decision %v, want decisionRestart", decision)
	}
}

// One stable port per repository: another worktree takes it over.
func TestClassifyExistingSwitchesToOtherWorktree(t *testing.T) {
	state := baseState()
	state.TargetWorktree = "/repo/other"
	state.TargetApp = "/repo/other"
	state.Branch = "other"

	decision, err := classifyExisting(state, baseTarget(), true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != decisionSwitch {
		t.Fatalf("got decision %v, want decisionSwitch", decision)
	}
}

// Switching stays inside one repository. Another repository's preview is never touched.
func TestClassifyExistingRefusesOtherRepositoryWorktree(t *testing.T) {
	state := baseState()
	state.CommonDir = "/elsewhere/.git"
	state.TargetWorktree = "/elsewhere/other"
	state.TargetApp = "/elsewhere/other"

	if _, err := classifyExisting(state, baseTarget(), true, true); err == nil {
		t.Fatal("expected an error when another repository owns the port")
	}
}

func TestClassifyExistingRefusesOtherRepository(t *testing.T) {
	state := baseState()
	state.CommonDir = "/elsewhere/.git"

	_, err := classifyExisting(state, baseTarget(), true, true)
	if err == nil {
		t.Fatal("expected an error when another repository owns the port")
	}
	if !strings.Contains(err.Error(), "another repository") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClassifyExistingClearsStaleState(t *testing.T) {
	state := baseState()
	state.PGID = deadProcessGroup

	decision, err := classifyExisting(state, baseTarget(), false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != decisionClearStale {
		t.Fatalf("got decision %v, want decisionClearStale", decision)
	}
}

// A live process group that no longer matches the recorded process must never be signalled.
func TestClassifyExistingRefusesUnverifiedProcessGroup(t *testing.T) {
	state := baseState()
	state.PGID = syscall.Getpgrp()

	_, err := classifyExisting(state, baseTarget(), false, false)
	if err == nil {
		t.Fatal("expected an error for an unverified process group")
	}
	if !strings.Contains(err.Error(), "unverified process group") {
		t.Errorf("unexpected error: %v", err)
	}
}
