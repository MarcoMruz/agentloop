package hitl

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func AskUser(toolName string, args map[string]any, title string, timeoutSec int) (Decision, error) {
	fmt.Print(FormatSummary(toolName, args, title))
	ch := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(os.Stdin)
		if s.Scan() {
			ch <- strings.TrimSpace(strings.ToLower(s.Text()))
		} else {
			ch <- "q"
		}
	}()
	timeout := time.Duration(timeoutSec) * time.Second
	if timeout == 0 {
		timeout = 300 * time.Second
	}
	select {
	case input := <-ch:
		switch input {
		case "a", "approve", "yes", "y":
			return DecisionApprove, nil
		case "s", "skip":
			return DecisionSkip, nil
		default:
			return DecisionAbort, nil
		}
	case <-time.After(timeout):
		return DecisionAbort, nil
	}
}
