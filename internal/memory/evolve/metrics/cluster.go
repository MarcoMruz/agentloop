package metrics

func ClusterOutcomes(outcomes []TaskOutcome, maxSize int) [][]TaskOutcome {
	if len(outcomes) == 0 {
		return nil
	}

	n := len(outcomes)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if connected(outcomes[i], outcomes[j]) {
				union(i, j)
			}
		}
	}

	groups := make(map[int][]int)
	for i := 0; i < n; i++ {
		r := find(i)
		groups[r] = append(groups[r], i)
	}

	var clusters [][]TaskOutcome
	for _, indices := range groups {
		var cluster []TaskOutcome
		for _, idx := range indices {
			cluster = append(cluster, outcomes[idx])
			if len(cluster) >= maxSize {
				clusters = append(clusters, cluster)
				cluster = nil
			}
		}
		if len(cluster) > 0 {
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

func connected(a, b TaskOutcome) bool {
	if len(a.TaskTopics) > 0 && len(b.TaskTopics) > 0 {
		for _, at := range a.TaskTopics {
			for _, bt := range b.TaskTopics {
				if at == bt {
					return true
				}
			}
		}
	}

	if len(a.TaskTopics) == 0 || len(b.TaskTopics) == 0 {
		shared := 0
		for _, ak := range a.TaskKeywords {
			for _, bk := range b.TaskKeywords {
				if ak == bk {
					shared++
					if shared >= 2 {
						return true
					}
					break
				}
			}
		}
	}

	return false
}

func FindClusterFor(clusters [][]TaskOutcome, sessionID string) []TaskOutcome {
	for _, c := range clusters {
		for _, o := range c {
			if o.SessionID == sessionID {
				return c
			}
		}
	}
	return nil
}
