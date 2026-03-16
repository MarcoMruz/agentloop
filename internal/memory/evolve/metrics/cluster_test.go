package metrics

import "testing"

func TestClusterBySharedTopic(t *testing.T) {
	outcomes := []TaskOutcome{
		{SessionID: "a", TaskTopics: []string{"auth", "security"}},
		{SessionID: "b", TaskTopics: []string{"auth", "token"}},
		{SessionID: "c", TaskTopics: []string{"deploy", "docker"}},
	}
	clusters := ClusterOutcomes(outcomes, 10)
	found := findClusterContaining(clusters, "a")
	if found == nil {
		t.Fatal("expected cluster containing a")
	}
	if !clusterContains(found, "b") {
		t.Fatal("a and b should be in same cluster (shared topic: auth)")
	}
	if clusterContains(found, "c") {
		t.Fatal("c should not be in same cluster as a")
	}
}

func TestClusterConnectedComponents(t *testing.T) {
	outcomes := []TaskOutcome{
		{SessionID: "a", TaskTopics: []string{"auth"}},
		{SessionID: "b", TaskTopics: []string{"auth", "database"}},
		{SessionID: "c", TaskTopics: []string{"database"}},
	}
	clusters := ClusterOutcomes(outcomes, 10)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (connected components), got %d", len(clusters))
	}
	if len(clusters[0]) != 3 {
		t.Fatalf("expected 3 outcomes in cluster, got %d", len(clusters[0]))
	}
}

func TestClusterNoTopicsFallsBackToKeywords(t *testing.T) {
	outcomes := []TaskOutcome{
		{SessionID: "a", TaskKeywords: []string{"auth", "token", "refresh"}},
		{SessionID: "b", TaskKeywords: []string{"auth", "token", "expire"}},
		{SessionID: "c", TaskKeywords: []string{"deploy", "docker", "kubernetes"}},
	}
	clusters := ClusterOutcomes(outcomes, 10)
	found := findClusterContaining(clusters, "a")
	if found == nil {
		t.Fatal("expected cluster containing a")
	}
	if !clusterContains(found, "b") {
		t.Fatal("a and b should cluster (2+ shared keywords: auth, token)")
	}
	if clusterContains(found, "c") {
		t.Fatal("c should not cluster with a (0 shared keywords)")
	}
}

func TestClusterMaxSize(t *testing.T) {
	var outcomes []TaskOutcome
	for i := 0; i < 20; i++ {
		outcomes = append(outcomes, TaskOutcome{
			SessionID:  string(rune('a' + i)),
			TaskTopics: []string{"shared"},
		})
	}
	clusters := ClusterOutcomes(outcomes, 5)
	for _, c := range clusters {
		if len(c) > 5 {
			t.Fatalf("cluster exceeds max size 5, got %d", len(c))
		}
	}
}

func TestClusterEmpty(t *testing.T) {
	clusters := ClusterOutcomes(nil, 10)
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(clusters))
	}
}

func findClusterContaining(clusters [][]TaskOutcome, sessionID string) []TaskOutcome {
	for _, c := range clusters {
		for _, o := range c {
			if o.SessionID == sessionID {
				return c
			}
		}
	}
	return nil
}

func clusterContains(cluster []TaskOutcome, sessionID string) bool {
	for _, o := range cluster {
		if o.SessionID == sessionID {
			return true
		}
	}
	return false
}
