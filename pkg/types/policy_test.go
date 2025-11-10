package types

import "testing"

func TestStaticTransitionPolicyValidate(t *testing.T) {
	policy := DefaultTransitionPolicy()

	if err := policy.Validate(LifecycleStatePending, LifecycleStateActive); err != nil {
		t.Fatalf("expected pending->active to be allowed: %v", err)
	}

	if err := policy.Validate(LifecycleStateActive, LifecycleStateArchived); err != nil {
		t.Fatalf("expected active->archived allowed: %v", err)
	}

	if err := policy.Validate(LifecycleStatePending, LifecycleStateArchived); err == nil {
		t.Fatalf("expected pending->archived to be rejected")
	}
}

func TestStaticTransitionPolicyAllowedTargets(t *testing.T) {
	policy := DefaultTransitionPolicy()
	targets := policy.AllowedTargets(LifecycleStateActive)
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets for active, got %d", len(targets))
	}
}
