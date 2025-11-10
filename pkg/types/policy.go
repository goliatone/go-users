package types

import (
	"fmt"
)

// ErrTransitionNotAllowed reports that the target lifecycle state is not
// reachable from the current state according to configured policies.
var ErrTransitionNotAllowed = fmt.Errorf("go-users: lifecycle transition not allowed")

// TransitionPolicy validates lifecycle transitions.
type TransitionPolicy interface {
	Validate(current, target LifecycleState) error
	AllowedTargets(current LifecycleState) []LifecycleState
}

// StaticTransitionPolicy enforces a fixed transition graph.
type StaticTransitionPolicy struct {
	graph map[LifecycleState]map[LifecycleState]struct{}
}

// NewStaticTransitionPolicy creates a policy from a transition graph.
func NewStaticTransitionPolicy(graph map[LifecycleState][]LifecycleState) *StaticTransitionPolicy {
	internal := make(map[LifecycleState]map[LifecycleState]struct{}, len(graph))
	for from, targets := range graph {
		targetSet := make(map[LifecycleState]struct{}, len(targets))
		for _, to := range targets {
			if to == "" {
				continue
			}
			targetSet[to] = struct{}{}
		}
		internal[from] = targetSet
	}
	return &StaticTransitionPolicy{graph: internal}
}

// DefaultTransitionPolicy returns the policy matching the upstream auth state
// machine (pending→active/disabled, active→suspended/disabled/archived, etc.).
func DefaultTransitionPolicy() *StaticTransitionPolicy {
	return NewStaticTransitionPolicy(map[LifecycleState][]LifecycleState{
		LifecycleStatePending:   {LifecycleStateActive, LifecycleStateDisabled},
		LifecycleStateActive:    {LifecycleStateSuspended, LifecycleStateDisabled, LifecycleStateArchived},
		LifecycleStateSuspended: {LifecycleStateActive, LifecycleStateDisabled},
		LifecycleStateDisabled:  {LifecycleStateArchived},
	})
}

// Validate ensures the target is allowed from the current state.
func (p *StaticTransitionPolicy) Validate(current, target LifecycleState) error {
	if current == "" || target == "" {
		return ErrTransitionNotAllowed
	}
	targets, ok := p.graph[current]
	if !ok {
		return ErrTransitionNotAllowed
	}
	if _, ok := targets[target]; !ok {
		return ErrTransitionNotAllowed
	}
	return nil
}

// AllowedTargets returns the slice of valid targets from the provided state.
func (p *StaticTransitionPolicy) AllowedTargets(current LifecycleState) []LifecycleState {
	targets := p.graph[current]
	if len(targets) == 0 {
		return nil
	}
	out := make([]LifecycleState, 0, len(targets))
	for target := range targets {
		out = append(out, target)
	}
	return out
}
