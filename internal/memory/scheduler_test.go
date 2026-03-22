package memory

import "testing"

func TestScheduleMinimal(t *testing.T) {
	level := Schedule("ok")
	if level != Minimal {
		t.Errorf("short task: want Minimal, got %v", level)
	}
}

func TestScheduleStandard(t *testing.T) {
	level := Schedule("fix the auth bug in the login service")
	if level != Standard {
		t.Errorf("medium task: want Standard, got %v", level)
	}
}

func TestScheduleDetailed(t *testing.T) {
	level := Schedule("please go through all the pull requests opened this week and leave a summary comment on each one describing what changed and why it might matter to the team")
	if level != Detailed {
		t.Errorf("complex task: want Detailed, got %v", level)
	}
}

func TestScheduleComplexityKeywords(t *testing.T) {
	level := Schedule("refactor the auth module")
	if level != Detailed {
		t.Errorf("task with complexity keyword 'refactor': want Detailed, got %v", level)
	}
}
